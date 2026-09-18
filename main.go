package main

import (
	"fmt"
	"os"

	"ocr-go/handlers" // ชื่อ Module ต้องตรงกับใน go.mod

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

func main() {
	router := gin.Default()

	// 1. ตั้งค่า CORS (อนุญาตให้ทุกโดเมนเรียกใช้งาน API ได้)
	router.Use(cors.Default())

	// 2. จำกัดขนาดหน่วยความจำสำหรับการอัปโหลดไฟล์ (8 MB)
	router.MaxMultipartMemory = 8 << 20

	// Routes
	router.GET("/api/v1/health", handlers.HealthCheck)

	// API Versions
	router.POST("/api/v1/ocr", handlers.UploadOCR)       // V1 (ตัวเดิม)
	router.POST("/api/v2/ocr", handlers.UploadOCRV2)     // V2 (คาดเดาคำผิด)
	router.POST("/api/v3/ocr", handlers.UploadOCRV3)     // V3 (ครอบตัดรูป + ดึงข้อมูลนักเรียน)

	// กำหนด Port
	port := os.Getenv("PORT")
	if port == "" {
		port = "8888"
	}

	fmt.Println("========================================")
	fmt.Println("         Go OCR API Started")
	fmt.Println("========================================")
	fmt.Printf("Server : http://localhost:%s\n", port)
	fmt.Println("GET    : /api/v1/health")
	fmt.Println("POST   : /api/v1/ocr (เวอร์ชัน 1 - default)")
	fmt.Println("POST   : /api/v2/ocr (เวอร์ชัน 2 - ระบบคาดเดาเกรดที่ผิดเพี้ยน)")
	fmt.Println("POST   : /api/v3/ocr (เวอร์ชัน 3 - ระบบครอบตัดรูปภาพเพื่อเพิ่มความแม่นยำ)")
	fmt.Println("========================================")

	if err := router.Run(":" + port); err != nil {
		panic(err)
	}
}