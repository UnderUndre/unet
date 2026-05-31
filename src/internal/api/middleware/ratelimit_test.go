package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/underundre/unet/internal/api/apicontext"
	"github.com/underundre/unet/internal/auth"
)

func TestRateLimiter_WithinLimit(t *testing.T) {
	now := time.Now()
	rl := NewRateLimiter(func() time.Time { return now })

	ok := 0
	for i := 0; i < 10; i++ {
		if rl.allow("token-1") {
			ok++
		}
	}
	if ok != 10 {
		t.Errorf("expected 10 allowed, got %d", ok)
	}
}

func TestRateLimiter_ExceedsLimit(t *testing.T) {
	now := time.Now()
	rl := NewRateLimiter(func() time.Time { return now })

	for i := 0; i < 10; i++ {
		rl.allow("token-1")
	}

	if rl.allow("token-1") {
		t.Error("expected 11th request to be rejected (burst=10)")
	}
}

func TestRateLimiter_IndependentTokens(t *testing.T) {
	now := time.Now()
	rl := NewRateLimiter(func() time.Time { return now })

	for i := 0; i < 10; i++ {
		rl.allow("token-1")
	}

	if !rl.allow("token-2") {
		t.Error("different tokens should have independent limits")
	}
}

func TestRateLimiter_TokenRefill(t *testing.T) {
	now := time.Now()
	rl := NewRateLimiter(func() time.Time { return now })

	for i := 0; i < 10; i++ {
		rl.allow("token-1")
	}

	if rl.allow("token-1") {
		t.Error("should be rate limited")
	}

	now = now.Add(2 * time.Second)
	if !rl.allow("token-1") {
		t.Error("should be allowed after token refill")
	}
}

func TestRateLimiter_Middleware_LoopbackSkipped(t *testing.T) {
	rl := NewRateLimiter(nil)
	called := false

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}))

	ctx := apicontext.WithAuthInfo(context.Background(), &apicontext.AuthInfo{
		TokenID: "stub",
		Scope:   string(auth.ScopeAdmin),
		Source:  "localhost",
	})

	req := httptest.NewRequest("GET", "/v1/peers", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if !called {
		t.Error("handler should be called for localhost")
	}
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestRateLimiter_Middleware_RateLimited(t *testing.T) {
	now := time.Now()
	rl := NewRateLimiter(func() time.Time { return now })

	handler := rl.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	ctx := apicontext.WithAuthInfo(context.Background(), &apicontext.AuthInfo{
		TokenID: "token-1",
		Scope:   string(auth.ScopeRead),
		Source:  "pat",
	})

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest("GET", "/v1/peers", nil).WithContext(ctx)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
	}

	req := httptest.NewRequest("GET", "/v1/peers", nil).WithContext(ctx)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusTooManyRequests {
		t.Errorf("expected 429, got %d", rec.Code)
	}
	if ra := rec.Header().Get("Retry-After"); ra != "60" {
		t.Errorf("expected Retry-After 60, got %s", ra)
	}
}
