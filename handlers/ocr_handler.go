package handlers

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"ocr-go/services" // แก้ไขชื่อ module ให้ตรงกับโปรเจกต์ของคุณ

	"github.com/gin-gonic/gin"
)

func HealthCheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"status":  "ok",
		"service": "go-ocr",
	})
}

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

	// เรียกใช้งาน Service
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
