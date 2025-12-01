package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

type rateLimiter struct {
	requests map[string]*clientRequest
	mu       sync.RWMutex
	limit    int
	window   time.Duration
}

type clientRequest struct {
	count     int
	resetTime time.Time
}

var limiter *rateLimiter

func init() {
	limiter = &rateLimiter{
		requests: make(map[string]*clientRequest),
		limit:    100,
		window:   time.Minute,
	}

	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			limiter.cleanup()
		}
	}()
}

func RateLimiter() gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()

		limiter.mu.Lock()
		defer limiter.mu.Unlock()

		client, exists := limiter.requests[ip]
		now := time.Now()

		if !exists || now.After(client.resetTime) {
			limiter.requests[ip] = &clientRequest{
				count:     1,
				resetTime: now.Add(limiter.window),
			}
			c.Next()
			return
		}

		if client.count >= limiter.limit {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":       "Rate limit exceeded",
				"retry_after": client.resetTime.Sub(now).Seconds(),
			})
			c.Abort()
			return
		}

		client.count++
		c.Next()
	}
}

func (rl *rateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, client := range rl.requests {
		if now.After(client.resetTime) {
			delete(rl.requests, ip)
		}
	}
}