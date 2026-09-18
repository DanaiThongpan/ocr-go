package handlers

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"ocr-go/models"
	"ocr-go/services" // แก้ไขชื่อ module ให้ตรงกับโปรเจกต์ของคุณ

	"github.com/gin-gonic/gin"
)

func HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"status":  "ok",
		"service": "go-ocr",
		"endpoints": gin.H{
			"v1": "POST /api/v1/ocr (เวอร์ชัน 1 - default)",
			"v2": "POST /api/v2/ocr (เวอร์ชัน 2 - ระบบคาดเดาเกรดที่ผิดเพี้ยน)",
			"v3": "POST /api/v3/ocr (เวอร์ชัน 3 - ระบบครอบตัดรูปภาพเพื่อเพิ่มความแม่นยำ)",
		},
	})
}

// =========================================================
// API v1
// =========================================================
func UploadOCR(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "กรุณาอัปโหลดไฟล์"})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".pdf" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "กรุณาอัปโหลดไฟล์ PDF เท่านั้น"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถเปิดไฟล์ได้"})
		return
	}
	defer src.Close()

	tempPDF, err := os.CreateTemp("", "go-ocr-*.pdf")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถสร้างไฟล์ชั่วคราวได้"})
		return
	}
	tempPDFPath := tempPDF.Name()
	defer os.Remove(tempPDFPath)

	_, err = io.Copy(tempPDF, src)
	tempPDF.Close()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถคัดลอกไฟล์ได้"})
		return
	}

	fullText, subjects, pageCount, err := services.ProcessPDF(tempPDFPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"data": gin.H{
			"filename": file.Filename,
			"pages":    pageCount,
			"subjects": subjects,
			"raw_text": fullText,
		},
	})
}

// =========================================================
// API v2
// =========================================================
func UploadOCRV2(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "กรุณาอัปโหลดไฟล์"})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".pdf" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "กรุณาอัปโหลดไฟล์ PDF เท่านั้น"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถเปิดไฟล์ได้"})
		return
	}
	defer src.Close()

	tempPDF, err := os.CreateTemp("", "go-ocr-v2-*.pdf")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถสร้างไฟล์ชั่วคราวได้"})
		return
	}
	tempPDFPath := tempPDF.Name()
	defer os.Remove(tempPDFPath)

	_, err = io.Copy(tempPDF, src)
	tempPDF.Close()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถคัดลอกไฟล์ได้"})
		return
	}

	fullText, _, pageCount, err := services.ProcessPDF(tempPDFPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	subjects := services.ExtractGradesV2(fullText)

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"version": "v2",
		"data": gin.H{
			"filename": file.Filename,
			"pages":    pageCount,
			"subjects": subjects,
			"raw_text": fullText,
		},
	})
}

// =========================================================
// API v3 (หั่นรูปภาพเฉพาะส่วน + ส่งข้อมูลส่วนตัวนักเรียน)
// =========================================================
func UploadOCRV3(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "กรุณาอัปโหลดไฟล์"})
		return
	}

	ext := strings.ToLower(filepath.Ext(file.Filename))
	if ext != ".pdf" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "กรุณาอัปโหลดไฟล์ PDF เท่านั้น"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถเปิดไฟล์ได้"})
		return
	}
	defer src.Close()

	tempPDF, err := os.CreateTemp("", "go-ocr-v3-*.pdf")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถสร้างไฟล์ชั่วคราวได้"})
		return
	}
	tempPDFPath := tempPDF.Name()
	defer os.Remove(tempPDFPath)

	_, err = io.Copy(tempPDF, src)
	tempPDF.Close()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถคัดลอกไฟล์ได้"})
		return
	}

	// เรียกใช้งาน ProcessPDFV3
// ⭐️ อัปเดตให้รับค่า debugImages เข้ามาด้วย
	fullText, subjects, prefix, firstName, lastName, nationalID, pageCount, err := services.ProcessPDFV3(tempPDFPath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, models.OCRResponse{
		Success: true,
		Version: "v3",
		Data: models.OCRResponseData{
			Filename:    file.Filename,
			Title:       prefix,
			FirstName:   firstName,
			LastName:    lastName,
			NationalID:  nationalID,
			Pages:       pageCount,
			Subjects:    subjects,
			RawText:     fullText,

		},
	})
}