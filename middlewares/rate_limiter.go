package middlewares

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/time/rate"
)

var (
	visitors = make(map[string]*visitor)
	mtx      sync.Mutex
)

type visitor struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

func getVisitor(ip string) *rate.Limiter {
	mtx.Lock()
	defer mtx.Unlock()

	v, exists := visitors[ip]
	if !exists {
		// e.g. 2 requests per second, burst of 5
		limiter := rate.NewLimiter(2, 5)
		visitors[ip] = &visitor{limiter, time.Now()}
		return limiter
	}

	v.lastSeen = time.Now()
	return v.limiter
}

func init() {
	go func() {
		for {
			time.Sleep(time.Minute)
			mtx.Lock()
			for ip, v := range visitors {
				if time.Since(v.lastSeen) > 3*time.Minute {
					delete(visitors, ip)
				}
			}
			mtx.Unlock()
		}
	}()
}

func IPRateLimiter() gin.HandlerFunc {
	return func(c *gin.Context) {
		limiter := getVisitor(c.ClientIP())
		if !limiter.Allow() {
			if Log != nil {
				Log.Printf("Rate limit exceeded for IP: %s\n", c.ClientIP())
			}
			c.JSON(http.StatusTooManyRequests, gin.H{
				"success": false,
				"error":   "Too Many Requests - Please slow down.",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
