package webapp

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"sync"
	"time"
)

type middleware func(http.Handler) http.Handler

func chainMiddleware(middlewares ...middleware) middleware {
	return func(next http.Handler) http.Handler {
		for index := len(middlewares) - 1; index >= 0; index-- {
			next = middlewares[index](next)
		}
		return next
	}
}

func recoverWrap() middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if recovered := recover(); recovered != nil {
					slog.ErrorContext(r.Context(), "panic in http handler", "error", recovered, "stack", string(debug.Stack()))
					w.WriteHeader(http.StatusInternalServerError)
					resp := map[string]any{
						"code":    -1,
						"message": "Internal Server Error",
					}
					if err := json.NewEncoder(w).Encode(resp); err != nil {
						slog.ErrorContext(r.Context(), "Failed to encode error response after panic", "error", err)
					}
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}

func logging() middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			lrw := &loggedResponseWriter{ResponseWriter: w, statusCode: http.StatusInternalServerError}
			start := time.Now()
			next.ServeHTTP(lrw, r)
			latency := time.Since(start)

			finalStatusCode := lrw.statusCode

			slog.DebugContext(r.Context(), "http request completed",
				"method", r.Method,
				"path", r.URL.Path,
				"status", finalStatusCode,
				"latency", latency)
		})
	}
}

// loggedResponseWriter is a custom ResponseWriter that captures the status code.
type loggedResponseWriter struct {
	http.ResponseWriter
	statusCode    int
	headerWritten bool
}

func (lrw *loggedResponseWriter) WriteHeader(code int) {
	if !lrw.headerWritten {
		lrw.statusCode = code
		lrw.headerWritten = true
		lrw.ResponseWriter.WriteHeader(code)
	}
}

func (lrw *loggedResponseWriter) Write(data []byte) (int, error) {
	if !lrw.headerWritten {
		lrw.statusCode = http.StatusOK
		lrw.headerWritten = true
	}
	return lrw.ResponseWriter.Write(data)
}

// rateLimiter implements a simple token bucket rate limiter per IP.
type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int           // requests per window
	window   time.Duration // time window
}

type visitor struct {
	tokens    int
	lastReset time.Time
}

func newRateLimiter(rate int, window time.Duration) *rateLimiter {
	rl := &rateLimiter{
		visitors: make(map[string]*visitor),
		rate:     rate,
		window:   window,
	}

	// Cleanup old entries periodically
	go rl.cleanup()

	return rl
}

func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(rl.window * 2)
	defer ticker.Stop()

	for range ticker.C {
		rl.mu.Lock()
		for ip, v := range rl.visitors {
			if time.Since(v.lastReset) > rl.window*2 {
				delete(rl.visitors, ip)
			}
		}
		rl.mu.Unlock()
	}
}

func (rl *rateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	v, exists := rl.visitors[ip]
	if !exists {
		rl.visitors[ip] = &visitor{
			tokens:    rl.rate - 1,
			lastReset: time.Now(),
		}
		return true
	}

	// Reset tokens if window has passed
	if time.Since(v.lastReset) > rl.window {
		v.tokens = rl.rate - 1
		v.lastReset = time.Now()
		return true
	}

	// Check if tokens available
	if v.tokens > 0 {
		v.tokens--
		return true
	}

	return false
}

// rateLimit creates a rate limiting middleware.
// rate: number of requests allowed per window
// window: time window for rate limiting
func rateLimit(rate int, window time.Duration) middleware {
	limiter := newRateLimiter(rate, window)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting for health checks
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			// Get client IP
			ip := r.RemoteAddr
			if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
				ip = forwarded
			}

			if !limiter.allow(ip) {
				slog.Warn("Rate limit exceeded", "ip", ip, "path", r.URL.Path)
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				json.NewEncoder(w).Encode(map[string]string{
					"error": "too many requests",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
