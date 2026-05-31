package middleware

import (
	"net/http"
	"sync"
	"time"

	"github.com/underundre/unet/internal/api/apicontext"
	"github.com/underundre/unet/internal/api/v1"
)

const (
	rateLimitPerMinute  = 60
	rateLimitBurst      = 10
	rateLimitWindow     = time.Minute
	rateLimitEvictAfter = 2 * time.Minute
)

type visitor struct {
	tokens    float64
	lastCheck time.Time
}

type RateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	nowFunc  func() time.Time
}

func NewRateLimiter(nowFunc func() time.Time) *RateLimiter {
	if nowFunc == nil {
		nowFunc = time.Now
	}
	return &RateLimiter{
		visitors: make(map[string]*visitor),
		nowFunc:  nowFunc,
	}
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		info, ok := apicontext.AuthInfoFromContext(r.Context())
		if !ok || info.TokenID == "" || info.Source == "localhost" {
			next.ServeHTTP(w, r)
			return
		}

		if !rl.allow(info.TokenID) {
			w.Header().Set("Retry-After", "60")
			v1.ErrorResponse(w, http.StatusTooManyRequests, v1.ErrCodeRateLimited,
				"Rate limit exceeded", map[string]any{"retryAfter": 60})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) allow(key string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := rl.nowFunc()

	if len(rl.visitors) > 1000 {
		for k, v := range rl.visitors {
			if now.Sub(v.lastCheck) > rateLimitEvictAfter {
				delete(rl.visitors, k)
			}
		}
	}

	v, ok := rl.visitors[key]
	if !ok {
		rl.visitors[key] = &visitor{
			tokens:    float64(rateLimitBurst - 1),
			lastCheck: now,
		}
		return true
	}

	elapsed := now.Sub(v.lastCheck)
	v.lastCheck = now
	v.tokens += float64(elapsed) * float64(rateLimitPerMinute) / float64(rateLimitWindow)
	if v.tokens > float64(rateLimitBurst) {
		v.tokens = float64(rateLimitBurst)
	}

	if v.tokens < 1 {
		return false
	}

	v.tokens--
	return true
}
