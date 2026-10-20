package handlers

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"ocr-go/models"
	"ocr-go/services" 

	"github.com/gin-gonic/gin"
)

// ฟังก์ชันช่วยเหลือสำหรับตรวจสอบนามสกุลไฟล์ที่อนุญาต
func isAllowedExtension(ext string) bool {
	allowed := map[string]bool{
		".pdf": true, ".jpg": true, ".jpeg": true, ".png": true, ".webp": true,
	}
	return allowed[ext]
}

func HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"status":  "ok",
		"service": "go-ocr",
		"endpoints": gin.H{
			"v3": "POST /api/v3/ocr (เวอร์ชัน 3 - ระบบครอบตัดรูปภาพเพื่อเพิ่มความแม่นยำ)",
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
	if !isAllowedExtension(ext) {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "รองรับเฉพาะไฟล์ PDF, JPG, JPEG, PNG และ WEBP เท่านั้น"})
		return
	}

	src, err := file.Open()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถเปิดไฟล์ได้"})
		return
	}
	defer src.Close()

	tempFile, err := os.CreateTemp("", "go-ocr-v3-*"+ext)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถสร้างไฟล์ชั่วคราวได้"})
		return
	}
	tempFilePath := tempFile.Name()
	defer os.Remove(tempFilePath)

	_, err = io.Copy(tempFile, src)
	tempFile.Close()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": "ไม่สามารถคัดลอกไฟล์ได้"})
		return
	}

	// เรียกใช้งาน ProcessPDFV3 
	fullText, gpax, subjects, prefix, firstName, lastName, nationalID, pageCount, err := services.ProcessPDFV3(tempFilePath)
    if err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"success": false, "error": err.Error()})
        return
    }

    c.JSON(http.StatusOK, models.OCRResponse{
        Success: true,
        Version: "v3",
        Data: models.OCRResponseData{
            Filename:   file.Filename,
            Title:      prefix,
            FirstName:  firstName,
            LastName:   lastName,
            NationalID: nationalID,
            Pages:      pageCount,
            Subjects:   subjects,
            GPAX:       gpax,
            RawText:    fullText,
        },
    })
}