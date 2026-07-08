package backend

import (
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// rateLimiter is a minimal fixed-window-per-key limiter, implemented with
// only the standard library so it doesn't need a new dependency (this
// sandbox can't reach the Go module proxy, and a real deployment shouldn't
// need to pull in a whole library just for this). It's fine for a single
// server instance; a multi-instance deployment would want a shared store
// (e.g. Redis) instead.
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
}

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: make(map[string][]time.Time), limit: limit, window: window}
}

func (rl *rateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	kept := rl.hits[key][:0]
	for _, t := range rl.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rl.limit {
		rl.hits[key] = kept
		return false
	}
	rl.hits[key] = append(kept, now)
	return true
}

// RateLimit builds Gin middleware that allows at most `limit` requests per
// `window` per client IP, returning 429 once exceeded.
//
// Previously nothing in the app had any request throttling at all, so
// /login/mock, the OAuth callback, and /add-account could all be hit as
// fast as a script could send requests - no brute-force/spam protection.
func RateLimit(limit int, window time.Duration) gin.HandlerFunc {
	rl := newRateLimiter(limit, window)
	return func(c *gin.Context) {
		if !rl.allow(c.ClientIP()) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Too many requests - please wait a moment and try again.",
			})
			c.Abort()
			return
		}
		c.Next()
	}
}
