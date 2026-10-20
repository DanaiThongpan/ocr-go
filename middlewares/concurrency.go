package middlewares

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

var semaphore chan struct{}

func InitConcurrency(maxWorkers int) {
	semaphore = make(chan struct{}, maxWorkers)
}

func ConcurrencyLimiter(timeout time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		select {
		case semaphore <- struct{}{}:
			// Acquired lock
			defer func() { <-semaphore }()
			c.Next()
		case <-time.After(timeout):
			// Timeout waiting for lock
			if Log != nil {
				Log.Printf("Concurrency limit exceeded - request timed out for IP: %s\n", c.ClientIP())
			}
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"error":   "Server is currently under heavy load. Please try again later.",
			})
			c.Abort()
			return
		}
	}
}
