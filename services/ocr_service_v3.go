package services

import (
	"fmt"
	"image"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"ocr-go/models"

	"github.com/otiai10/gosseract/v2"
)

// =========================================================
// V3: Image Cropping & Student Info Extraction
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
		// ⭐️ โหมดตัดเอาเฉพาะครึ่งล่าง
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

func ExtractStudentInfo(text string) (prefix, firstName, lastName, nationalID string) {
	reNatID := regexp.MustCompile(`(\d{1})[\-\s]*(\d{4})[\-\s]*(\d{5})[\-\s]*(\d{2})[\-\s]*(\d{1})`)
	idMatch := reNatID.FindStringSubmatch(text)
	if len(idMatch) >= 6 {
		nationalID = fmt.Sprintf("%s-%s-%s-%s-%s", idMatch[1], idMatch[2], idMatch[3], idMatch[4], idMatch[5])
	}

	prefixRegex := `(?:ชื่อ\s*(นาย|นางสาว|นาง|เด็กชาย|เด็กหญิง|ด\.ช\.|ด\.ญ\.)?|(นาย|นางสาว|นาง|เด็กชาย|เด็กหญิง|ด\.ช\.|ด\.ญ\.))`
	reName := regexp.MustCompile(prefixRegex + `\s*([ก-๙a-zA-Z]+)`)
	nameMatch := reName.FindStringSubmatch(text)
	if len(nameMatch) >= 4 {
		p := nameMatch[1]
		if p == "" {
			p = nameMatch[2]
		}
		prefix = p
		firstName = nameMatch[3]
	}

	reLastName := regexp.MustCompile(`(?:ชื่อสกุล|นามสกุล|สกุล)\s*([ก-๙a-zA-Z]+)`)
	lastNameMatch := reLastName.FindStringSubmatch(text)
	if len(lastNameMatch) >= 2 {
		lastName = lastNameMatch[1]
	}

	if lastName == "" {
		reFullName := regexp.MustCompile(prefixRegex + `\s*([ก-๙a-zA-Z]+)\s+([ก-๙a-zA-Z]+)`)
		fullMatch := reFullName.FindStringSubmatch(text)
		if len(fullMatch) >= 5 {
			p := fullMatch[1]
			if p == "" {
				p = fullMatch[2]
			}
			prefix = p
			firstName = fullMatch[3]
			lastName = fullMatch[4]
		}
	}

	return prefix, firstName, lastName, nationalID
}

// =========================================================
// ProcessPDFV3 Orchestrator
// =========================================================
func ProcessPDFV3(pdfPath string) (string, []models.SubjectGradeV3, string, string, string, string, int, error) {
	outputDir := os.TempDir()
	outputPrefix := filepath.Join(outputDir, "go-ocr-img-v3")

	cmd := exec.Command("pdftoppm", "-png", "-r", "800", pdfPath, outputPrefix)
	if err := cmd.Run(); err != nil {
		return "", nil, "", "", "", "", 0, fmt.Errorf("แปลง PDF ไม่สำเร็จ: %v", err)
	}

	matches, err := filepath.Glob(fmt.Sprintf("%s-*.png", outputPrefix))
	if err != nil || len(matches) == 0 {
		return "", nil, "", "", "", "", 0, fmt.Errorf("ไม่พบไฟล์รูปภาพที่ถูกแปลงจาก PDF")
	}

	defer func() {
		for _, m := range matches {
			os.Remove(m)
		}
	}()

	// 1. หั่นรูปภาพ
	for i, imagePath := range matches {
		if i == 0 {
			// หน้า 1: ตัด 1/4
			cropImageByMode(imagePath, "top_one_quarter")
		} else if i == 1 {
			// ⭐️ หน้า 2: นำการตัด 3 สเต็ปแบบเดิมกลับมา
			cropImageByMode(imagePath, "right_half")
			cropImageByMode(imagePath, "top_70")
			cropImageByMode(imagePath, "bottom_half")
		}
	}

	debugDir, _ := filepath.Abs("debug_images")
	os.MkdirAll(debugDir, os.ModePerm)

	fmt.Println("\n------------------------------------------------")
	for i, imagePath := range matches {
		imgData, err := os.ReadFile(imagePath)
		if err == nil {
			debugPath := filepath.Join(debugDir, fmt.Sprintf("page_%d.png", i+1))
			errWrite := os.WriteFile(debugPath, imgData, 0644)
			if errWrite == nil {
				fmt.Printf("📸 [DEBUG] บันทึกรูปหน้าที่ %d แล้วที่: %s\n", i+1, debugPath)
			}
		}
	}
	fmt.Println("------------------------------------------------\n")

	client := gosseract.NewClient()
	defer client.Close()
	client.SetLanguage("tha", "eng")
	
	client.SetPageSegMode(gosseract.PSM_SINGLE_BLOCK)
	client.SetVariable("user_defined_dpi", "800")

	var fullTextBuilder strings.Builder
	var page2Text string

	// 2. ทำ OCR
	for i, imagePath := range matches {
		if client.SetImage(imagePath) == nil {
			if text, err := client.Text(); err == nil {
				fullTextBuilder.WriteString(text)
				fullTextBuilder.WriteString("\n\n---PAGE---\n\n")
				if i == 1 {
					page2Text = text
				}
			}
		}
	}

	fullText := fullTextBuilder.String()

	// 3. ดึงเกรด
	var subjects []models.SubjectGradeV3
	if len(matches) > 1 {
		subjects = ExtractGradesV3(page2Text)
	} else {
		subjects = ExtractGradesV3(fullText)
	}

	// 4. ดึงข้อมูลนักเรียน
	prefix, firstName, lastName, nationalID := ExtractStudentInfo(fullText)

	return fullText, subjects, prefix, firstName, lastName, nationalID, len(matches), nil
}

// =========================================================
// Helper V3: ฟังก์ชันดึงตัวเลขจากข้อความแบบแม่นยำสูง
// =========================================================
func extractNumbers(line string) (float64, float64, bool) {
	// ❌ เอา reSpaceDec ตรงนี้ออกไปเลย เพราะมันดึงเลข "40" กับ "3.75" มาผสมกันเป็น "40.3.75"
	// ทำให้วิชาภาษาไทยหาเกรดไม่เจอ! เราจะไปดักแก้พวกช่องว่างที่ ExtractGradesV3 แทน

	numberRegex := regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)`)
	matches := numberRegex.FindAllString(line, -1)

	if len(matches) >= 2 {
		for j := len(matches) - 1; j >= 1; j-- {
			creditStr := matches[j-1]
			gpaStr := matches[j]

			if j == len(matches)-1 && (gpaStr == "0" || gpaStr == "0.0" || gpaStr == "0.00") && len(matches) >= 3 {
				continue
			}

			credit, err1 := strconv.ParseFloat(creditStr, 64)
			gpa, err2 := strconv.ParseFloat(gpaStr, 64)

			if err1 == nil && err2 == nil {
				// ซ่อมแซม GPA
				if gpa > 4.0 && gpa <= 400 {
					if gpa >= 100 {
						gpa = gpa / 100.0
					} else if gpa >= 10 {
						gpa = gpa / 10.0
					}
				}
				if gpa > 4.0 {
					continue
				}

				// ⭐️ ซ่อมแซม Credit ให้ฉลาดขึ้น ครอบคลุม 40, 14.0, 10.0
				if credit >= 10 && !strings.Contains(creditStr, ".") {
					credit = credit / 10.0 // เช่น "40" -> "4.0"
				}
				
				// ถ้าหน่วยกิตอ่านติดมาเป็นหลักสิบ (เช่น 14.0 หรือ 10.0) จับหาร 10 ให้หมด
				for credit >= 10.0 {
					credit = credit / 10.0
				}

				gpa = math.Round(gpa*100) / 100
				credit = math.Round(credit*100) / 100

				if gpa <= 4.0 && credit > 0 {
					return credit, gpa, true
				}
			}
		}
	}
	return 0, 0, false
}

// =========================================================
// V3: Extract Grades (ระบบล็อกเป้าหมาย + เติมข้อมูลในช่องว่าง + Fuzzy Match)
// =========================================================
func ExtractGradesV3(text string) []models.SubjectGradeV3 {
	// ดักจับคำเพี้ยนที่เกิดจาก DPI 1200
	text = strings.ReplaceAll(text, "ป", "4.0")
	text = strings.ReplaceAll(text, "ฯ", "4.00")
	text = strings.ReplaceAll(text, "ผลตี", "4.00")
	text = strings.ReplaceAll(text, "7วว", "4.00")
	text = strings.ReplaceAll(text, "ร8", "")

	// ⭐️ 1. ซ่อมจุดทศนิยมที่ OCR อ่านเพี้ยนเป็นเลข 8 (เช่น "28 5|" -> "2.5|")
	reDotReadAsEight := regexp.MustCompile(`(\d)8\s+([0-9])\s*\|`)
	text = reDotReadAsEight.ReplaceAllString(text, "${1}.${2}|")
	reDotReadAsEightBracket := regexp.MustCompile(`(\d)8\s+([0-9])\s*]`)
	text = reDotReadAsEightBracket.ReplaceAllString(text, "${1}.${2}]")

	// ⭐️ 2. ซ่อมช่องว่างที่ทศนิยมหายไป (เช่น "10 0|" -> "10.0|")
	reMissingDotSpace := regexp.MustCompile(`(\d)\s+([05])\s*\|`)
	text = reMissingDotSpace.ReplaceAllString(text, "${1}.${2}|")
	reMissingDotSpaceBracket := regexp.MustCompile(`(\d)\s+([05])\s*]`)
	text = reMissingDotSpaceBracket.ReplaceAllString(text, "${1}.${2}]")

	reComma := regexp.MustCompile(`(\d),(\d)`)
	text = reComma.ReplaceAllString(text, "${1}.${2}")

	thaiDigits := []string{"๐", "๑", "๒", "๓", "๔", "๕", "๖", "๗", "๘", "๙"}
	arabicDigits := []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9"}
	for i, td := range thaiDigits {
		text = strings.ReplaceAll(text, td, arabicDigits[i])
	}

	badChars := []string{"|", "]", "}", ")", "[", "(", "\"", "'"}
	for _, char := range badChars {
		text = strings.ReplaceAll(text, char, " ")
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
						if isTaken { continue }

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
					if bestSlot != -1 {
						mappedSlot[vIdx] = bestSlot
					}
				}
			}
		}
	}

	for i, sIdx := range mappedSlot {
		if sIdx != -1 {
			results[sIdx].Credits = floatPtr(validLines[i].credit)
			results[sIdx].GPA = floatPtr(validLines[i].gpa)
		}
	}

	return results
}