package middlewares

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

var Log *log.Logger

func InitLogger() {
	file, err := os.OpenFile("server.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0666)
	if err == nil {
		Log = log.New(file, "", log.LstdFlags)
	} else {
		Log = log.New(os.Stderr, "", log.LstdFlags)
	}
}

func SetupLogHook() {}

type LogPayload struct {
	Timestamp    time.Time   `json:"timestamp"`
	Status       int         `json:"status"`
	LatencyMs    int64       `json:"latency_ms"`
	FileNames    []string    `json:"file_names"`
	TotalSizeMB  float64     `json:"total_size_mb"`
	IP           string      `json:"ip"`
	Method       string      `json:"method"`
	Path         string      `json:"path"`
	ErrorMessage string      `json:"error_message"`
	APIResponse  interface{} `json:"api_response"`
	FileBase64   string      `json:"file_base64"`
}

type bodyLogWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w bodyLogWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func OCRLoggerMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		reqIP := c.ClientIP()

		var fileNames []string
		var totalSize int64
		var fileBase64 string

		// Extract form safely and encode the first file
		form, err := c.MultipartForm()
		if err == nil && form != nil {
			if files, ok := form.File["file"]; ok {
				for i, fileHeader := range files {
					fileNames = append(fileNames, fileHeader.Filename)
					totalSize += fileHeader.Size

					// Read and encode the first file to Base64 Data URI
					if i == 0 {
						f, err := fileHeader.Open()
						if err == nil {
							data, err := io.ReadAll(f)
							f.Close()
							if err == nil {
								mime := fileHeader.Header.Get("Content-Type")
								if mime == "" || mime == "application/octet-stream" {
									ext := strings.ToLower(filepath.Ext(fileHeader.Filename))
									switch ext {
									case ".pdf":
										mime = "application/pdf"
									case ".png":
										mime = "image/png"
									case ".jpg", ".jpeg":
										mime = "image/jpeg"
									case ".webp":
										mime = "image/webp"
									default:
										mime = "application/octet-stream"
									}
								}
								fileBase64 = fmt.Sprintf("data:%s;base64,%s", mime, base64.StdEncoding.EncodeToString(data))
							}
						}
					}
				}
			}
		}

		// Intercept Response
		blw := &bodyLogWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
		c.Writer = blw

		c.Next()

		latency := time.Since(start)
		status := c.Writer.Status()
		sizeMB := float64(totalSize) / 1024.0 / 1024.0

		// Extract Error Message or API Response
		var apiResp interface{}
		var errMsg string
		respBody := blw.body.Bytes()

		if len(respBody) > 0 {
			var jsonMap map[string]interface{}
			if err := json.Unmarshal(respBody, &jsonMap); err == nil {
				if status >= 400 {
					if e, ok := jsonMap["error"].(string); ok {
						errMsg = e
					} else {
						errMsg = string(respBody) // fallback
					}
				} else {
					apiResp = jsonMap
				}
			} else {
				if status >= 400 {
					errMsg = string(respBody) // pure text error
				}
			}
		}

		// Prepare JSON payload for SSE
		payload := LogPayload{
			Timestamp:    time.Now(),
			Status:       status,
			LatencyMs:    latency.Milliseconds(),
			FileNames:    fileNames,
			TotalSizeMB:  sizeMB,
			IP:           reqIP,
			Method:       c.Request.Method,
			Path:         c.Request.URL.Path,
			ErrorMessage: errMsg,
			APIResponse:  apiResp,
			FileBase64:   fileBase64,
		}

		payloadBytes, _ := json.Marshal(payload)
		Broker.Broadcast(string(payloadBytes))

		// Console printing in Thai
		var statusColor string
		var statusText string
		var statusIcon string

		if status >= 200 && status < 300 {
			statusColor = "\033[32m" // Green
			statusText = "สำเร็จ"
			statusIcon = "🟢"
		} else if status >= 400 && status < 500 {
			statusColor = "\033[33m" // Yellow
			statusText = "ข้อผิดพลาดฝั่งผู้ใช้"
			statusIcon = "🟡"
		} else if status >= 500 {
			statusColor = "\033[31m" // Red
			statusText = "ข้อผิดพลาดระบบ"
			statusIcon = "🔴"
		} else {
			statusColor = "\033[36m" // Cyan
			statusText = "ดำเนินการ"
			statusIcon = "🔵"
		}
		resetColor := "\033[0m"

		fileList := "ไม่มีไฟล์"
		if len(fileNames) > 0 {
			fileList = strings.Join(fileNames, ", ")
		}

		latencySec := float64(latency.Milliseconds()) / 1000.0

		fmt.Printf("[บันทึกระบบ OCR] %s %s%s (รหัส %d)%s\n", statusIcon, statusColor, statusText, status, resetColor)
		fmt.Printf("   ├─ ⏱️  เวลาประมวลผล : %.2f วินาที\n", latencySec)
		fmt.Printf("   ├─ 📁 ไฟล์ที่อัปโหลด : %s (ขนาด %.2f MB)\n", fileList, sizeMB)
		fmt.Printf("   └─ 💻 ผู้เรียกใช้งาน : %s\n", reqIP)
		fmt.Printf("------------------------------------------------\n")
		
		if Log != nil {
			Log.Printf("Status: %d, Latency: %.2fs, Files: %s, IP: %s\n", status, latencySec, fileList, reqIP)
		}
	}
}
