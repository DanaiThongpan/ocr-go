package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"ocr-go/handlers"
	"ocr-go/middlewares"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

const (
	maxMemory     = 32 << 20 // 32 MB
	maxUploadSize = 50 << 20 // 50 MB
)

func limitUploadSize(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

func main() {
	// 1. Initialize Middlewares
	middlewares.InitLogger()
	middlewares.SetupLogHook()
	middlewares.InitConcurrency(4) // Max 4 concurrent ProcessPDFV3

	// Disable default gin logger since we have a custom structured one
	gin.SetMode(gin.ReleaseMode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(cors.Default())
	router.MaxMultipartMemory = maxMemory

	// Dashboard UI
	router.StaticFile("/", "./index.html")

	// Health and Live Logs
	router.GET("/api/v3/health", handlers.HealthCheck)
	router.GET("/logs/live", handlers.LiveLogs)

	// API Versions
	ocr := router.Group("/api", limitUploadSize(maxUploadSize))
	// Apply Global Protection & Logging Middlewares
	ocr.Use(middlewares.IPRateLimiter())
	ocr.Use(middlewares.OCRLoggerMiddleware())
	ocr.Use(middlewares.ConcurrencyLimiter(15 * time.Second)) // Wait in queue max 15s
	{
		ocr.POST("/v3/ocr", handlers.UploadOCRV3)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8888"
	}

	fmt.Printf("========================================\n")
	fmt.Printf("         Go OCR API Started\n")
	fmt.Printf("========================================\n")
	fmt.Printf("Server : http://localhost:%s\n", port)
	fmt.Printf("Max upload size : %d MB\n", maxUploadSize>>20)
	fmt.Printf("Dashboard : http://localhost:%s/\n", port)
	fmt.Printf("Live logs : http://localhost:%s/logs/live\n", port)
	fmt.Printf("========================================\n")

	if err := router.Run(":" + port); err != nil {
		log.Fatal(err)
	}
}
