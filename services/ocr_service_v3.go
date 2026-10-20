package services

import (
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"sync"

	"ocr-go/models"

	"github.com/otiai10/gosseract/v2"
	"gocv.io/x/gocv"
)

// =========================================================
// Page limit
// =========================================================
const MaxPDFPages = 2

// TooManyPagesError ใช้ให้ handler แยกแยะกรณีไฟล์เกินจำนวนหน้า
type TooManyPagesError struct {
	Pages int
	Max   int
}

func (e *TooManyPagesError) Error() string {
	return fmt.Sprintf("ไฟล์มีจำนวน %d หน้า โปรดตรวจสอบไฟล์เอกสาร (รองรับเอกสารไม่เกิน %d หน้า)", e.Pages, e.Max)
}

// getPDFPageCount นับจำนวนหน้า PDF ด้วย pdfinfo (ไม่ต้องแปลงเป็นรูป จึงเร็วมาก)
func getPDFPageCount(filePath string) (int, error) {
	out, err := exec.Command("pdfinfo", filePath).Output()
	if err != nil {
		return 0, fmt.Errorf("อ่านข้อมูล PDF ไม่ได้: %v", err)
	}
	m := regexp.MustCompile(`(?m)^Pages:\s+(\d+)`).FindSubmatch(out)
	if len(m) < 2 {
		return 0, fmt.Errorf("ไม่พบจำนวนหน้าในไฟล์ PDF")
	}
	return strconv.Atoi(string(m[1]))
}

// =========================================================
// Helper: รวมคำที่อยู่แนวนอนเดียวกันให้เป็นบรรทัดเดียว
// PSM_SPARSE_TEXT มักแยก "หน่วยกิต" กับ "เกรด" เป็นคนละบรรทัด (เช่น "4.0" / "4.00")
// ทำให้ extractNumbers ที่ต้องการตัวเลข 2 ตัวในบรรทัดเดียวอ่านแถวนั้นไม่ได้
// ฟังก์ชันนี้จัดแถวใหม่จากพิกัดของแต่ละคำ (ไม่ขึ้นกับการแบ่งบรรทัดของ Tesseract)
// =========================================================
func rebuildRowsFromBoxes(boxes []gosseract.BoundingBox) string {
	type word struct {
		cy, x int
		text  string
	}

	var words []word
	var heights []int
	for _, b := range boxes {
		t := strings.TrimSpace(b.Word)
		if t == "" {
			continue
		}
		words = append(words, word{
			cy:   (b.Box.Min.Y + b.Box.Max.Y) / 2,
			x:    b.Box.Min.X,
			text: t,
		})
		heights = append(heights, b.Box.Dy())
	}
	if len(words) == 0 {
		return ""
	}

	// ค่ากลางของความสูงตัวอักษร ใช้เป็นเกณฑ์ว่าอยู่ "แถวเดียวกัน" หรือไม่
	sort.Ints(heights)
	tol := float64(heights[len(heights)/2]) * 0.6
	if tol < 8 {
		tol = 8
	}

	sort.Slice(words, func(i, j int) bool { return words[i].cy < words[j].cy })

	var rows [][]word
	cur := []word{words[0]}
	sum := float64(words[0].cy)
	for _, w := range words[1:] {
		mean := sum / float64(len(cur))
		if float64(w.cy)-mean > tol {
			rows = append(rows, cur)
			cur = []word{w}
			sum = float64(w.cy)
		} else {
			cur = append(cur, w)
			sum += float64(w.cy)
		}
	}
	rows = append(rows, cur)

	var sb strings.Builder
	for _, r := range rows {
		sort.Slice(r, func(i, j int) bool { return r[i].x < r[j].x })
		parts := make([]string, len(r))
		for k, w := range r {
			parts[k] = w.text
		}
		sb.WriteString(strings.Join(parts, " "))
		sb.WriteString("\n")
	}
	return sb.String()
}

// =========================================================
// Helper: ฟังก์ชันตรวจจับความคมชัดของภาพ
// =========================================================
func getSharpnessScore(filePath string) float64 {
	file, err := os.Open(filePath)
	if err != nil {
		return 0
	}
	defer file.Close()

	img, err := png.Decode(file)
	if err != nil {
		return 0
	}

	bounds := img.Bounds()
	w, h := bounds.Dx(), bounds.Dy()
	if w < 3 || h < 3 {
		return 0
	}

	gray := make([]float64, w*h)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.GrayModel.Convert(img.At(bounds.Min.X+x, bounds.Min.Y+y)).(color.Gray)
			gray[y*w+x] = float64(c.Y)
		}
	}

	var sum, count float64
	laplacian := make([]float64, w*h)
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			idx := y*w + x
			val := gray[(y-1)*w+x] + gray[(y+1)*w+x] + gray[y*w+(x-1)] + gray[y*w+(x+1)] - 4*gray[idx]
			laplacian[idx] = val
			sum += val
			count++
		}
	}
	mean := sum / count

	var variance float64
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			diff := laplacian[y*w+x] - mean
			variance += diff * diff
		}
	}

	if count == 0 {
		return 0
	}
	return variance / count
}

// =========================================================
// Helper: ฟังก์ชันลบเส้นตารางด้วย OpenCV
// =========================================================
func RemoveLinesWithOpenCV(imagePath string) error {
	img := gocv.IMRead(imagePath, gocv.IMReadGrayScale)
	if img.Empty() {
		return fmt.Errorf("cannot read image")
	}

	// 1. Intelligent Image Resizing (OpenCV Downscaling)
	if img.Cols() > 2000 {
		ratio := float64(2000) / float64(img.Cols())
		newWidth := 2000
		newHeight := int(float64(img.Rows()) * ratio)
		
		resized := gocv.NewMat()
		gocv.Resize(img, &resized, image.Pt(newWidth, newHeight), 0, 0, gocv.InterpolationArea)
		img.Close() // Release large image
		img = resized // Use resized
	}
	defer img.Close()

	bw := gocv.NewMat()
	defer bw.Close()
	gocv.AdaptiveThreshold(img, &bw, 255, gocv.AdaptiveThresholdGaussian, gocv.ThresholdBinaryInv, 15, 5)

	horizontal := bw.Clone()
	defer horizontal.Close()
	hKernel := gocv.GetStructuringElement(gocv.MorphRect, image.Pt(img.Cols()/40, 1))
	defer hKernel.Close()
	gocv.MorphologyEx(horizontal, &horizontal, gocv.MorphOpen, hKernel)

	vertical := bw.Clone()
	defer vertical.Close()
	vKernel := gocv.GetStructuringElement(gocv.MorphRect, image.Pt(1, img.Rows()/40))
	defer vKernel.Close()
	gocv.MorphologyEx(vertical, &vertical, gocv.MorphOpen, vKernel)

	lines := gocv.NewMat()
	defer lines.Close()
	gocv.Add(horizontal, vertical, &lines)

	dilateKernel := gocv.GetStructuringElement(gocv.MorphRect, image.Pt(3, 3))
	defer dilateKernel.Close()
	gocv.Dilate(lines, &lines, dilateKernel)

	// ⭐️ เดิมใช้ gocv.Add(img, lines) ซึ่งทำให้เส้นกลายเป็นสีขาวสนิท (255)
	// ถ้าพื้นเอกสารเป็นสีเทา (ภาพถ่าย/สแกนไม่ขาว) Tesseract จะใช้ Otsu threshold
	// แยก "เส้นขาว" ออกจาก "พื้นเทา+ตัวอักษร" ทำให้ทั้งหน้ากลายเป็นสีดำ อ่านอะไรไม่ได้
	// วิธีใหม่: ระบายเส้นด้วยสีพื้นหลังบริเวณนั้น (median blur ไม่ถูกเส้นบาง ๆ รบกวน)
	bg := gocv.NewMat()
	defer bg.Close()
	gocv.MedianBlur(img, &bg, 31)
	bg.CopyToWithMask(&img, lines)

	gocv.IMWrite(imagePath, img)
	return nil
}

// =========================================================
// V3: Image Cropping
// =========================================================
func cropImageByMode(filePath string, mode string) error {
	file, err := os.Open(filePath)
	if err != nil {
		return err
	}
	img, err := png.Decode(file)
	file.Close()
	if err != nil {
		return err
	}

	bounds := img.Bounds()
	var cropRect image.Rectangle

	if mode == "top_half" {
		cropRect = image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Max.X, bounds.Min.Y+(bounds.Dy()/2))
	} else if mode == "right_half" {
		cropRect = image.Rect(bounds.Min.X+(bounds.Dx()/2), bounds.Min.Y, bounds.Max.X, bounds.Max.Y)
	} else if mode == "top_70" {
		cropRect = image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Max.X, bounds.Min.Y+int(float64(bounds.Dy())*0.7))
	} else if mode == "top_one_quarter" {
		cropRect = image.Rect(bounds.Min.X, bounds.Min.Y, bounds.Max.X, bounds.Min.Y+(bounds.Dy()/4))
	} else if mode == "bottom_half" {
		cropRect = image.Rect(bounds.Min.X, bounds.Min.Y+(bounds.Dy()/2), bounds.Max.X, bounds.Max.Y)
	} else {
		return nil
	}

	type subImager interface {
		SubImage(r image.Rectangle) image.Image
	}
	sImg, ok := img.(subImager)
	if !ok {
		return fmt.Errorf("image does not support cropping")
	}
	croppedImg := sImg.SubImage(cropRect)

	outFile, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer outFile.Close()
	return png.Encode(outFile, croppedImg)
}

// =========================================================
// Helper: ฟังก์ชันดึงข้อมูลนักเรียน
// =========================================================
func ExtractStudentInfo(text string) (prefix, firstName, lastName, nationalID string) {
	// Filter out lines containing parent keywords to avoid picking up parent names
	lines := strings.Split(text, "\n")
	var validLines []string
	ignoreWords := []string{"บิดา", "มารดา", "ผู้ปกครอง"}
	
	for _, line := range lines {
		skip := false
		for _, w := range ignoreWords {
			if strings.Contains(line, w) {
				skip = true
				break
			}
		}
		if !skip {
			// clean up trailing dots like ........... and underscores
			line = regexp.MustCompile(`\.{2,}`).ReplaceAllString(line, " ")
			line = regexp.MustCompile(`_{2,}`).ReplaceAllString(line, " ")
			line = strings.TrimSpace(line)
			validLines = append(validLines, line)
		}
	}
	filteredText := strings.Join(validLines, "\n")

	reNatIDStrict := regexp.MustCompile(`(\d{1})[\-\s]*(\d{4})[\-\s]*(\d{5})[\-\s]*(\d{2})[\-\s]*(\d{1})`)
	if idMatch := reNatIDStrict.FindStringSubmatch(filteredText); len(idMatch) >= 6 {
		nationalID = fmt.Sprintf("%s-%s-%s-%s-%s", idMatch[1], idMatch[2], idMatch[3], idMatch[4], idMatch[5])
	} else {
		reNatIDLoose := regexp.MustCompile(`ประชาชน\s*([0-9\-\s]{8,17})`)
		if looseMatch := reNatIDLoose.FindStringSubmatch(filteredText); len(looseMatch) >= 2 {
			nationalID = strings.TrimSpace(looseMatch[1])
		}
	}

	prefixRegex := `(?:(?:ชื่อ|รื่อ|ซื่อ|ชือ)\s*(นาย|นางสาว|นาง|เด็กชาย|เด็กหญิง|ด\.ช\.|ด\.ญ\.|นพสว|น\.ส\.)?|(นาย|นางสาว|นาง|เด็กชาย|เด็กหญิง|ด\.ช\.|ด\.ญ\.|นพสว|น\.ส\.))`
	reName := regexp.MustCompile(prefixRegex + `\s*([ก-๙a-zA-Z]+)`)
	nameMatch := reName.FindStringSubmatch(filteredText)
	if len(nameMatch) >= 4 {
		p := nameMatch[1]
		if p == "" {
			p = nameMatch[2]
		}
		if p == "นพสว" || p == "น.ส." {
			p = "นางสาว"
		} else if p == "ด.ช." {
			p = "เด็กชาย"
		} else if p == "ด.ญ." {
			p = "เด็กหญิง"
		}
		prefix = p
		firstName = nameMatch[3]
	}

	reLastName := regexp.MustCompile(`(?:ชื่อสกุล|นามสกุล|สกุล|สกล)\s*([ก-๙a-zA-Z]+)`)
	lastNameMatch := reLastName.FindStringSubmatch(filteredText)
	if len(lastNameMatch) >= 2 {
		lastName = lastNameMatch[1]
	}

	if lastName == "" {
		reFullName := regexp.MustCompile(prefixRegex + `\s*([ก-๙a-zA-Z]+)\s+([ก-๙a-zA-Z]+)`)
		fullMatch := reFullName.FindStringSubmatch(filteredText)
		if len(fullMatch) >= 5 {
			p := fullMatch[1]
			if p == "" {
				p = fullMatch[2]
			}
			if p == "นพสว" || p == "น.ส." {
				p = "นางสาว"
			} else if p == "ด.ช." {
				p = "เด็กชาย"
			} else if p == "ด.ญ." {
				p = "เด็กหญิง"
			}
			prefix = p
			firstName = fullMatch[3]
			lastName = fullMatch[4]
		}
	}

	return prefix, firstName, lastName, nationalID
}

// =========================================================
// Helper: ฟังก์ชันดึงเกรดเฉลี่ยสะสม
// =========================================================
func ExtractGPAX(text string) *float64 {
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		if strings.Contains(line, "เฉลี่ย") || strings.Contains(strings.ToUpper(line), "GPAX") {
			reFloat := regexp.MustCompile(`\d+\.\d{1,2}`)
			matches := reFloat.FindAllString(line, -1)

			for i := len(matches) - 1; i >= 0; i-- {
				if gpax, err := strconv.ParseFloat(matches[i], 64); err == nil {
					if gpax > 0.0 && gpax <= 4.00 {
						return &gpax
					}
				}
			}
		}
	}
	return nil
}

// =========================================================
// ProcessPDFV3 Orchestrator
// =========================================================
func ProcessPDFV3(filePath string) (string, *float64, []models.SubjectGradeV3, string, string, string, string, int, error) {
	ext := strings.ToLower(filepath.Ext(filePath))
	var matches []string

	isPDF := ext == ".pdf"

	if isPDF {
		// ⭐️ เช็กจำนวนหน้าก่อนแปลง ถ้าเกินให้หยุดทันที
		if pageCount, err := getPDFPageCount(filePath); err == nil && pageCount > MaxPDFPages {
			return "", nil, nil, "", "", "", "", pageCount,
				&TooManyPagesError{Pages: pageCount, Max: MaxPDFPages}
		}

		taskDir, err := os.MkdirTemp("", "ocr-v3-task-*")
		if err != nil {
			return "", nil, nil, "", "", "", "", 0, fmt.Errorf("สร้างโฟลเดอร์ชั่วคราวไม่ได้: %v", err)
		}
		defer os.RemoveAll(taskDir)

		outputPrefix := filepath.Join(taskDir, "page")
		// -f 1 -l 3 : กันเหนียว แปลงไม่เกิน 3 หน้า เผื่อ pdfinfo อ่านไม่ได้
		cmd := exec.Command("pdftoppm", "-png", "-r", "300", "-f", "1", "-l", fmt.Sprint(MaxPDFPages+1), filePath, outputPrefix)
		if err := cmd.Run(); err != nil {
			return "", nil, nil, "", "", "", "", 0, fmt.Errorf("แปลง PDF ไม่สำเร็จ: %v", err)
		}

		matches, err = filepath.Glob(fmt.Sprintf("%s-*.png", outputPrefix))
		if err != nil || len(matches) == 0 {
			return "", nil, nil, "", "", "", "", 0, fmt.Errorf("ไม่พบไฟล์รูปภาพที่ถูกแปลงจาก PDF")
		}

		// เช็กซ้ำหลังแปลง (กรณี pdfinfo ใช้ไม่ได้)
		if len(matches) > MaxPDFPages {
			return "", nil, nil, "", "", "", "", len(matches),
				&TooManyPagesError{Pages: len(matches), Max: MaxPDFPages}
		}
	} else {
		matches = append(matches, filePath)
	}

	debugDir, _ := filepath.Abs("debug_images")
	os.RemoveAll(debugDir)
	os.MkdirAll(debugDir, os.ModePerm)

	fmt.Println("\n------------------------------------------------")

	type pageResult struct {
		index     int
		text      string
		gradeRows string
		err       error
	}

	var wg sync.WaitGroup
	resultCh := make(chan pageResult, len(matches))

	for i, imagePath := range matches {
		wg.Add(1)
		go func(pageIndex int, imgPath string) {
			defer wg.Done()
			var pRes pageResult
			pRes.index = pageIndex

			// 1. OpenCV Lines Removal
			_ = RemoveLinesWithOpenCV(imgPath)

			// 2. Crop
			if pageIndex == 0 {
				cropImageByMode(imgPath, "top_one_quarter")
			} else if pageIndex == 1 {
				cropImageByMode(imgPath, "right_half")
				cropImageByMode(imgPath, "top_70")
				cropImageByMode(imgPath, "bottom_half")
			}

			// 3. Debug Image Saving
			if imgData, err := os.ReadFile(imgPath); err == nil {
				debugPath := filepath.Join(debugDir, fmt.Sprintf("page_%d.png", pageIndex+1))
				os.WriteFile(debugPath, imgData, 0644)
			}

			// 4. Tesseract OCR
			client := gosseract.NewClient()
			defer client.Close() // Strict Memory Management
			client.SetLanguage("tha", "eng")
			client.SetPageSegMode(gosseract.PSM_SPARSE_TEXT)
			client.SetVariable("preserve_interword_spaces", "1")

			sharpness := getSharpnessScore(imgPath)
			dynamicDPI := "300"
			if sharpness < 150 {
				dynamicDPI = "800"
			} else if sharpness < 400 {
				dynamicDPI = "600"
			} else if sharpness < 1000 {
				dynamicDPI = "400"
			} else {
				dynamicDPI = "300"
			}

			fmt.Printf("📸 [INFO] หน้า %d - ความคมชัด (Variance): %.2f -> ปรับ DPI Tesseract เป็น: %s\n", pageIndex+1, sharpness, dynamicDPI)
			client.SetVariable("user_defined_dpi", dynamicDPI)

			if err := client.SetImage(imgPath); err == nil {
				if text, err := client.Text(); err == nil {
					pRes.text = text
				}
				if pageIndex == 1 || len(matches) == 1 {
					if boxes, err := client.GetBoundingBoxes(gosseract.RIL_WORD); err == nil {
						pRes.gradeRows = rebuildRowsFromBoxes(boxes)
					}
				}
			}

			resultCh <- pRes
		}(i, imagePath)
	}

	wg.Wait()
	close(resultCh)

	// Collect results safely to ensure proper ordering
	pageResults := make([]pageResult, len(matches))
	for res := range resultCh {
		pageResults[res.index] = res
	}

	var fullTextBuilder strings.Builder
	var page2Text string
	var gradeRows string

	for i, res := range pageResults {
		if res.text != "" {
			fullTextBuilder.WriteString(res.text)
			fullTextBuilder.WriteString("\n\n---PAGE---\n\n")
		}
		if i == 1 {
			page2Text = res.text
			gradeRows = res.gradeRows
		} else if len(matches) == 1 && i == 0 {
			gradeRows = res.gradeRows
		}
	}
	fmt.Println("------------------------------------------------")

	fullText := fullTextBuilder.String()

	// GPAX: ลองจากแถวที่จัดใหม่ก่อน (ได้บรรทัด "ผลการเรียนเฉลี่ย... 65.0 3.77" ครบ) แล้วค่อย fallback
	var gpax *float64
	if gradeRows != "" {
		gpax = ExtractGPAX(gradeRows)
	}
	if gpax == nil {
		gpax = ExtractGPAX(fullText)
	}

	// เกรดรายวิชา: ใช้แถวที่จัดใหม่ ถ้าไม่มีค่อยใช้ข้อความเดิม
	gradeText := fullText
	if len(matches) > 1 {
		gradeText = page2Text
	}
	if gradeRows != "" {
		gradeText = gradeRows
	}
	subjects := ExtractGradesV3(gradeText)

	prefix, firstName, lastName, nationalID := ExtractStudentInfo(fullText)

	// ⭐️ เงื่อนไขใหม่: เช็กว่าระบบดึงอะไรมาได้บ้างไหม ถ้าว่างเปล่าทั้งหมดให้แจ้งเตือน
	hasValidData := false
	if firstName != "" || lastName != "" || nationalID != "" || gpax != nil {
		hasValidData = true
	}
	for _, s := range subjects {
		if s.Credits != nil || s.GPA != nil {
			hasValidData = true
			break
		}
	}

	if !hasValidData {
		return fullText, gpax, subjects, prefix, firstName, lastName, nationalID, len(matches), fmt.Errorf("ไม่สามารถอ่านข้อมูลสำคัญในไฟล์ได้ โปรดตรวจสอบไฟล์เอกสาร")
	}

	return fullText, gpax, subjects, prefix, firstName, lastName, nationalID, len(matches), nil
}

// =========================================================
// Helper V3: ฟังก์ชันดึงตัวเลข
// =========================================================
func extractNumbers(line string) (float64, float64, bool) {
	numberRegex := regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)`)
	matches := numberRegex.FindAllString(line, -1)

	if len(matches) >= 2 {
		for j := len(matches) - 1; j >= 1; j-- {
			creditStr := matches[j-1]
			gpaStr := matches[j]
			creditIdx := j - 1

			if j == len(matches)-1 && (gpaStr == "0" || gpaStr == "0.0" || gpaStr == "0.00") && len(matches) >= 3 {
				continue
			}

			if creditIdx >= 1 && !strings.Contains(creditStr, ".") && !strings.Contains(gpaStr, ".") {
				if len(gpaStr) == 2 || len(gpaStr) == 1 {
					gpaStr = creditStr + "." + gpaStr
					creditIdx--
					creditStr = matches[creditIdx]
				}
			}

			if creditIdx >= 1 && !strings.Contains(creditStr, ".") && !strings.Contains(matches[creditIdx-1], ".") {
				if creditStr == "5" || creditStr == "0" || creditStr == "50" || creditStr == "00" {
					creditStr = matches[creditIdx-1] + "." + creditStr
					creditIdx--
				}
			}

			credit, err1 := strconv.ParseFloat(creditStr, 64)
			gpa, err2 := strconv.ParseFloat(gpaStr, 64)

			if err1 == nil && err2 == nil {
				if gpa > 4.0 {
					if gpa == 600 || gpa == 6.0 || gpa == 100 {
						gpa = 4.00
					} else if gpa >= 100 && gpa <= 400 {
						gpa = gpa / 100.0
					} else if gpa >= 10 && gpa <= 40 {
						gpa = gpa / 10.0
					}
				}

				if gpa > 4.0 || gpa < 0 {
					continue
				}

				if !strings.Contains(creditStr, ".") {
					if credit >= 100 {
						credit = credit / 10.0
					} else if credit >= 10 {
						credit = credit / 10.0
					}
				}

				credit = math.Round(credit*2) / 2
				gpa = math.Round(gpa*100) / 100

				if gpa >= 0.0 && gpa <= 4.0 && credit > 0 && credit <= 200 {
					return credit, gpa, true
				}
			}
		}
	}
	return 0, 0, false
}

// =========================================================
// V3: Extract Grades
// =========================================================
func ExtractGradesV3(text string) []models.SubjectGradeV3 {
	badChars := []string{"|", "]", "}", ")", "[", "(", "\"", "'", ","}
	for _, char := range badChars {
		text = strings.ReplaceAll(text, char, " ")
	}

	// ⭐️ placeholder ต้องไม่มีตัวอักษรที่ replacer ด้านล่างแปลงเป็นตัวเลข (O,o,Q,D,S,s,l,I,i,B,Z,z,v,V,G,b)
	// เดิมใช้ "##IS##" แล้ว I,S ถูกแปลงเป็น "##15##" ทำให้เกิดตัวเลขปลอม 1 และ 15 (-> credits 1, gpa 1.5)
	text = regexp.MustCompile(`(?i)\bis\b`).ReplaceAllString(text, "##KXK##")
	text = regexp.MustCompile(`(?i)\bgpa\b`).ReplaceAllString(text, "##HKH##")

	replacer := strings.NewReplacer(
		"O", "0", "o", "0", "Q", "0", "D", "0",
		"S", "5", "s", "5",
		"l", "1", "I", "1", "i", "1",
		"B", "8",
		"Z", "2", "z", "2",
		"v", "2", "V", "2",
		"G", "6", "b", "6",
	)
	text = replacer.Replace(text)

	// ⭐️ ต้องใช้ [ \t] ไม่ใช่ \s เพราะ \s กินตัวขึ้นบรรทัดใหม่ด้วย
	// เดิมอักขระขยะ "ป" ที่หัวบรรทัด (เช่น "\nป   คณิตศาสตร์ 11.5 3.80") ถูกแทนเป็น " 4.00 "
	// ทำให้แถวภาษาไทยกับแถวคณิตศาสตร์ถูกรวมเป็นบรรทัดเดียว ค่าทุกวิชาเลยเลื่อนไปหนึ่งช่อง
	reFixGradeP := regexp.MustCompile(`[ \t](ป|ฯ|ผลตี|7วว)[ \t]`)
	text = reFixGradeP.ReplaceAllString(text, " 4.00 ")
	reFixGradePEnd := regexp.MustCompile(`(?m)[ \t](ป|ฯ|ผลตี|7วว)[ \t]*$`)
	text = reFixGradePEnd.ReplaceAllString(text, " 4.00")

	text = strings.ReplaceAll(text, "##KXK##", "(IS)")
	text = strings.ReplaceAll(text, "##HKH##", "GPA")
	text = strings.ReplaceAll(text, "..", ".")

	thaiDigits := []string{"๐", "๑", "๒", "๓", "๔", "๕", "๖", "๗", "๘", "๙"}
	arabicDigits := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}
	for i, td := range thaiDigits {
		text = strings.ReplaceAll(text, td, arabicDigits[i])
	}

	lines := strings.Split(text, "\n")

	subjectNames := []string{
		"ภาษาไทย", "คณิตศาสตร์", "วิทยาศาสตร์และเทคโนโลยี",
		"สังคมศึกษา ศาสนา และวัฒนธรรม", "สุขศึกษาและพลศึกษา", "ศิลปะ",
		"การงานอาชีพ", "ภาษาต่างประเทศ", "การศึกษาค้นคว้าด้วยตนเอง (IS)",
	}

	anchors := [][]string{
		{"ภาษาไทย", "ษาไทย", "ภาษ", "ไทย", "ภาษา", "าไทย", "ทศไทย"},
		{"คณิตศาสตร์", "คณิต", "ศาสตร", "คณิ", "ตศาสตร์", "คณต", "ณตศาส"},
		{"วิทยาศาสตร์", "ทยาศาสตร์", "วิทยาศาสตร์และเทคโนโลยี", "เทคโนโลยี", "วิทยา", "เทคโน", "โลยี", "และเทค", "วทยา", "วทยาศาส", "ทคโนโลยี"},
		{"สังคม", "ศาสนา", "วัฒนธรรม", "สังคมศึกษา", "และวัฒน", "ธรรม", "คมศึกษา", "สงคม", "วฒนธร"},
		{"สุขศึกษา", "พลศึกษา", "สุขศึ", "พลศึ", "และพล", "ขศึกษา", "สขศกษา", "สขศก"},
		{"ศิลปะ", "ศลปะ", "ศิลป", "ศลป", "ศล", "ลปะ"},
		{"การงาน", "อาชีพ", "การงานอา", "งานอาชีพ", "การงา", "อาชพ"},
		{"อังกฤษ", "ต่างประเทศ", "ภาษาต่าง", "ต่างประ", "เทศ", "อังก", "องกฤษ", "ต่างประเท", "ตางประเทศ"},
		{"การศึกษา", "ค้นคว้า", "is", "ด้วยตนเอง", "(5)", "(is)", "ค้น", "คว้า", "ด้วยตน", "ษาค้น", "ตนเอง"},
	}

	results := make([]models.SubjectGradeV3, len(subjectNames))
	for i, name := range subjectNames {
		results[i] = models.SubjectGradeV3{
			Name:    name,
			Credits: nil,
			GPA:     nil,
		}
	}

	type parsedLine struct {
		text   string
		credit float64
		gpa    float64
	}
	var validLines []parsedLine
	for _, line := range lines {
		if strings.Contains(line, "เฉลี่ย") || strings.Contains(strings.ToUpper(line), "GPAX") || strings.Contains(line, "หลักสูตร") {
			continue
		}

		c, g, ok := extractNumbers(line)
		if ok {
			validLines = append(validLines, parsedLine{
				text:   strings.ToLower(strings.ReplaceAll(line, " ", "")),
				credit: c,
				gpa:    g,
			})
		}
	}

	mappedSlot := make([]int, len(validLines))
	for i := range mappedSlot {
		mappedSlot[i] = -1
	}
	// ⭐️ เก็บว่าบรรทัดไหนถูกจับคู่ด้วยชื่อวิชา (anchor) จริง ๆ ไม่ใช่การเดาจากลำดับ
	anchored := make([]bool, len(validLines))

	lastLockedSlot := -1
	for i, vLine := range validLines {
		for slotIdx := lastLockedSlot + 1; slotIdx < len(subjectNames); slotIdx++ {
			found := false
			for _, kw := range anchors[slotIdx] {
				if strings.Contains(vLine.text, kw) {
					found = true
					break
				}
			}
			if found {
				mappedSlot[i] = slotIdx
				anchored[i] = true
				lastLockedSlot = slotIdx
				break
			}
		}
	}

	type lockPoint struct {
		vIdx int
		sIdx int
	}
	locks := []lockPoint{{-1, -1}}
	for i, sIdx := range mappedSlot {
		if sIdx != -1 {
			locks = append(locks, lockPoint{i, sIdx})
		}
	}
	locks = append(locks, lockPoint{len(validLines), len(subjectNames)})

	reThaiText := regexp.MustCompile(`[ก-๙a-zA-Z]+`)

	for i := 0; i < len(locks)-1; i++ {
		l1 := locks[i]
		l2 := locks[i+1]

		vGapCount := l2.vIdx - l1.vIdx - 1
		sGapCount := l2.sIdx - l1.sIdx - 1

		if vGapCount > 0 {
			if vGapCount == sGapCount {
				for j := 0; j < vGapCount; j++ {
					mappedSlot[l1.vIdx+1+j] = l1.sIdx + 1 + j
				}
			} else {
				for vj := 1; vj <= vGapCount; vj++ {
					vIdx := l1.vIdx + vj
					textToMatch := validLines[vIdx].text
					blocks := reThaiText.FindAllString(textToMatch, -1)

					bestSlot := -1
					maxSim := -1.0

					for sj := 1; sj <= sGapCount; sj++ {
						sIdx := l1.sIdx + sj

						isTaken := false
						for _, taken := range mappedSlot {
							if taken == sIdx {
								isTaken = true
								break
							}
						}
						if isTaken {
							continue
						}

						for _, kw := range anchors[sIdx] {
							kwClean := strings.ToLower(strings.ReplaceAll(kw, " ", ""))
							sim := similarity(textToMatch, kwClean)
							if sim > maxSim {
								maxSim = sim
								bestSlot = sIdx
							}
							for _, block := range blocks {
								if len(block) >= 2 {
									blockSim := similarity(block, kwClean)
									if blockSim > maxSim {
										maxSim = blockSim
										bestSlot = sIdx
									}
								}
							}
						}
					}

					if bestSlot != -1 && maxSim >= 0.3 {
						mappedSlot[vIdx] = bestSlot
					} else {
						targetSlot := l1.sIdx + vj
						if targetSlot < l2.sIdx {
							isTaken := false
							for _, taken := range mappedSlot {
								if taken == targetSlot {
									isTaken = true
									break
								}
							}
							if !isTaken {
								mappedSlot[vIdx] = targetSlot
							}
						}
					}
				}
			}
		}
	}

	// ⭐️ วิชา IS (ช่องสุดท้าย) มักไม่มีเกรด (แสดงเป็น "-")
	// ถ้าไม่เจอชื่อวิชาในบรรทัดจริง ๆ ห้ามเติมค่าจากการเดาตามลำดับ ให้เป็น null
	isSlot := len(subjectNames) - 1

	for i, sIdx := range mappedSlot {
		if sIdx == isSlot && !anchored[i] {
			continue
		}
		if sIdx != -1 {
			results[sIdx].Credits = floatPtr(validLines[i].credit)
			results[sIdx].GPA = floatPtr(validLines[i].gpa)
		}
	}

	return results
}

// =========================================================
// V3 Helper Functions
// =========================================================
func floatPtr(v float64) *float64 {
	return &v
}

func levenshtein(a, b string) int {
	aRunes, bRunes := []rune(a), []rune(b)
	n, m := len(aRunes), len(bRunes)
	if n == 0 {
		return m
	}
	if m == 0 {
		return n
	}

	d := make([][]int, n+1)
	for i := range d {
		d[i] = make([]int, m+1)
		d[i][0] = i
	}
	for j := 0; j <= m; j++ {
		d[0][j] = j
	}

	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			cost := 1
			if aRunes[i-1] == bRunes[j-1] {
				cost = 0
			}
			minVal := d[i-1][j] + 1
			if d[i][j-1]+1 < minVal {
				minVal = d[i][j-1] + 1
			}
			if d[i-1][j-1]+cost < minVal {
				minVal = d[i-1][j-1] + cost
			}
			d[i][j] = minVal
		}
	}
	return d[n][m]
}

func similarity(a, b string) float64 {
	aRunes, bRunes := []rune(a), []rune(b)
	maxLen := len(aRunes)
	if len(bRunes) > maxLen {
		maxLen = len(bRunes)
	}
	if maxLen == 0 {
		return 1.0
	}
	return 1.0 - (float64(levenshtein(a, b)) / float64(maxLen))
}
