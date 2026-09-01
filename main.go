package main

import (
	"fmt"
	"os"
	"ocr-go/handlers" // ชื่อ Module ต้องตรงกับใน go.mod
	"github.com/gin-gonic/gin"
)

func main() {
	router := gin.Default()

	// Routes
	router.GET("/api/v1/health", handlers.HealthCheck)
	router.POST("/api/v1/ocr", handlers.UploadOCR)

	// กำหนด Port
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	fmt.Println("========================================")
	fmt.Println("         Go OCR API Started")
	fmt.Println("========================================")
	fmt.Printf("Server : http://localhost:%s\n", port)
	fmt.Println("GET    : /api/v1/health")
	fmt.Println("POST   : /api/v1/ocr")
	fmt.Println("========================================")

	if err := router.Run(":" + port); err != nil {
		panic(err)
	}
}