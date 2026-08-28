package main

import (
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/otiai10/gosseract/v2"
)

// =========================================================
// Model
// =========================================================

type SubjectGrade struct {
	Name    string  `json:"name"`
	Credits float64 `json:"credits"`
	GPA     float64 `json:"gpa"`
}

// =========================================================
// Main
// =========================================================

func main() {

	router := gin.Default()

	// =====================================================
	// Health Check
	// =====================================================
	router.GET("/api/v1/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"status":  "ok",
			"service": "go-ocr",
		})
	})

	// =====================================================
	// OCR API (รองรับ PDF ทุกหน้า)
	// =====================================================
	router.POST("/api/v1/ocr", func(c *gin.Context) {

		// -------------------------------------------------
		// รับไฟล์จาก Request
		// -------------------------------------------------
		file, err := c.FormFile("file")
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "กรุณาอัปโหลดไฟล์"})
			return
		}

		// ตรวจสอบนามสกุลไฟล์
		ext := strings.ToLower(filepath.Ext(file.Filename))
		if ext != ".pdf" {
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "กรุณาอัปโหลดไฟล์ PDF เท่านั้น"})
			return
		}

		// เปิดไฟล์ที่ Upload
		src, err := file.Open()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถเปิดไฟล์ได้"})
			return
		}
		defer src.Close()

		// สร้าง Temporary File สำหรับเก็บ PDF
		tempPDF, err := os.CreateTemp("", "go-ocr-*.pdf")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถสร้างไฟล์ชั่วคราวได้"})
			return
		}
		tempPDFPath := tempPDF.Name()
		defer os.Remove(tempPDFPath) // ลบไฟล์ PDF ชั่วคราวหลังจบงาน

		// คัดลอกข้อมูลไปที่ Temporary File
		_, err = io.Copy(tempPDF, src)
		tempPDF.Close() // ต้องปิดไฟล์ก่อนส่งให้โปรแกรมอื่นใช้งาน
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถคัดลอกไฟล์ได้"})
			return
		}

		// -------------------------------------------------
		// แปลง PDF ทุกหน้าเป็นรูปภาพด้วย pdftoppm
		// -------------------------------------------------
		outputDir := os.TempDir()
		outputPrefixPath := filepath.Join(outputDir, "go-ocr-img")
		
		// รัน command แปลง pdf เป็น png
		cmd := exec.Command("pdftoppm", "-png", tempPDFPath, outputPrefixPath)
		if err := cmd.Run(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": fmt.Sprintf("แปลง PDF ไม่สำเร็จ: %v (ตรวจสอบว่าติดตั้ง poppler-utils หรือยัง)", err)})
			return
		}

		// ค้นหาไฟล์รูปภาพที่ถูกสร้างขึ้นทั้งหมด
		matches, err := filepath.Glob(fmt.Sprintf("%s-*.png", outputPrefixPath))
		if err != nil || len(matches) == 0 {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่พบไฟล์รูปภาพที่ถูกแปลงจาก PDF"})
			return
		}
		
		// ลบรูปภาพทั้งหมดเมื่อ API ทำงานเสร็จ
		defer func() {
			for _, m := range matches {
				os.Remove(m)
			}
		}()

		// -------------------------------------------------
		// เตรียมตัวเอนจิน OCR
		// -------------------------------------------------
		client := gosseract.NewClient()
		defer client.Close()
		
		err = client.SetLanguage("tha", "eng")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": fmt.Sprintf("ไม่สามารถตั้งค่าภาษา OCR ได้: %v", err)})
			return
		}

		// -------------------------------------------------
		// อ่านข้อความจากรูปภาพทีละหน้ามาต่อกัน
		// -------------------------------------------------
		var fullTextBuilder strings.Builder
		for _, imagePath := range matches {
			if err := client.SetImage(imagePath); err == nil {
				if text, err := client.Text(); err == nil {
					fullTextBuilder.WriteString(text)
					fullTextBuilder.WriteString("\n\n---PAGE---\n\n")
				}
			}
		}

		fullText := fullTextBuilder.String()

		// -------------------------------------------------
		// วิเคราะห์และแยกข้อมูลเกรด
		// -------------------------------------------------
		subjects := extractGrades(fullText)

		// -------------------------------------------------
		// ส่ง Response กลับ
		// -------------------------------------------------
		c.JSON(http.StatusOK, gin.H{
			"success": true,
			"data": gin.H{
				"filename": file.Filename,
				"pages":    len(matches),
				"subjects": subjects,
				"raw_text": fullText, // คงไว้สำหรับใช้ตรวจสอบ (Debug)
			},
		})
	})

	// =====================================================
	// Start Server
	// =====================================================
	fmt.Println("========================================")
	fmt.Println("          Go OCR API Started")
	fmt.Println("========================================")
	fmt.Println("Server : http://localhost:8080")
	fmt.Println("GET    : /api/v1/health")
	fmt.Println("POST   : /api/v1/ocr")
	fmt.Println("========================================")
	
	err := router.Run(":8080")
	if err != nil {
		panic(err)
	}
}

// =========================================================
// Extract Grades (อัปเกรด: รับมือกับ OCR ขยะขั้นสุดยอด)
// =========================================================

func extractGrades(text string) []SubjectGrade {
	results := []SubjectGrade{}
	lines := strings.Split(text, "\n")

	foundSubjects := make(map[string]bool)

	// ชื่อวิชาที่ต้องการ และคำค้นหาที่อาจจะเพี้ยนจาก OCR
	searchTargets := []struct {
		StandardName string
		Keywords     []string
	}{
		{"ภาษาไทย", []string{"ภาษาไทย"}},
		{"คณิตศาสตร์", []string{"คณิตศาสตร์"}},
		{"วิทยาศาสตร์", []string{"วิทยาศาสตร์และเทคโนโลยี", "วิทยาศาสตร์"}},
		{"สังคมศึกษา ศาสนา และวัฒนธรรม", []string{"สังคมศึกษา ศาสนา และวัฒนธรรม", "สังคมศึกษา"}},
		{"สุขศึกษาและพลศึกษา", []string{"สุขศึกษาและพลศึกษา", "สุขศึกษา"}},
		{"ศิลปะ", []string{"ศิลปะ"}},
		{"การงานอาชีพและเทคโนโลยี", []string{"การงานอาชีพและเทคโนโลยี", "การงานอาชีพ"}},
		{"ภาษาต่างประเทศ", []string{"ภาษาต่างประเทศ", "ภาษาอังกฤษ"}},
	}

	// Regex หาตัวเลข (ยอมรับตัวเลขที่อาจมีจุดทศนิยม และอาจมีเครื่องหมายขยะติดมาบ้าง)
	numberRegex := regexp.MustCompile(`([0-9]+(?:\.[0-9]+)?)`)

	// ⭐️ อ่านจากล่างขึ้นบน
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}

		for _, target := range searchTargets {
			if foundSubjects[target.StandardName] {
				continue
			}

			// ลองหาด้วยทุก Keyword ที่เป็นไปได้ของวิชานี้
			var foundKeyword string
			var idx int = -1
			for _, kw := range target.Keywords {
				idx = strings.Index(line, kw)
				if idx != -1 {
					foundKeyword = kw
					break
				}
			}

			if idx != -1 {
				// 1. ดึงข้อความด้านขวาของชื่อวิชา
				afterText := line[idx+len(foundKeyword):]

				// 2. ล้างข้อมูล (Sanitize) แบบ Aggressive
				// ลบทุกอย่างที่ไม่ใช่ ตัวเลข, จุด, หรือช่องว่าง
				afterText = regexp.MustCompile(`[^0-9\.\s]`).ReplaceAllString(afterText, " ")

				// 3. ดึงตัวเลขทั้งหมดออกมา
				matches := numberRegex.FindAllString(afterText, -1)

				// 4. วิเคราะห์ตัวเลข
				if len(matches) >= 2 {
					// มักจะเป็น (หน่วยกิต, เกรด) เสมอ
					creditStr := matches[0]
					gpaStr := matches[1]

					credit, errCredit := strconv.ParseFloat(creditStr, 64)
					gpa, errGpa := strconv.ParseFloat(gpaStr, 64)

					if errCredit != nil || errGpa != nil {
						continue
					}

					// ⭐️ Heuristic 1: แก้ไขหน่วยกิตที่ OCR ลืมจุด (เช่น 40 -> 4.0, 115 -> 11.5)
					if credit >= 10 && !strings.Contains(creditStr, ".") {
						if credit >= 100 {
							credit = credit / 10.0 // เช่น 115 -> 11.5
						} else {
							credit = credit / 10.0 // เช่น 40 -> 4.0
						}
					}

					// ⭐️ Heuristic 2: แก้ไขเกรดที่ OCR ลืมจุด (เช่น 400 -> 4.00, 391 -> 3.91)
					if gpa >= 10 && !strings.Contains(gpaStr, ".") {
						gpa = gpa / 100.0
					}

					// ⭐️ Heuristic 3: ปัดเศษเกรดให้เหลือ 2 ตำแหน่ง
					gpa = math.Round(gpa*100) / 100

					// ตรวจสอบความถูกต้องขั้นสุดท้าย (เกรดต้องอยู่ระหว่าง 0 - 4.0)
					if gpa <= 4.0 && credit > 0 {
						results = append(results, SubjectGrade{
							Name:    target.StandardName,
							Credits: credit,
							GPA:     gpa,
						})
						foundSubjects[target.StandardName] = true
					}
				}
			}
		}
	}

	// พลิก Array ให้กลับมาเรียงจากบนลงล่างตามปกติ
	for i, j := 0, len(results)-1; i < j; i, j = i+1, j-1 {
		results[i], results[j] = results[j], results[i]
	}

	// (Optional) จัดเรียงลำดับวิชาให้คงที่เสมอ
	orderedResults := []SubjectGrade{}
	for _, target := range searchTargets {
		for _, r := range results {
			if r.Name == target.StandardName {
				orderedResults = append(orderedResults, r)
				break
			}
		}
	}

	return orderedResults
}