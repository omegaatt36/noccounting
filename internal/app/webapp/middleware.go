package webapp

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
	"time"
)

type middleware func(http.Handler) http.Handler

func chainMiddleware(middlewares ...middleware) middleware {
	return func(next http.Handler) http.Handler {
		for _, middleware := range slices.Backward(middlewares) {
			next = middleware(next)
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
			// Skip logging for health checks; probes would flood the log.
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

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

type rateLimiter struct {
	mu       sync.Mutex
	visitors map[string]*visitor
	rate     int           // requests per window
	window   time.Duration // time window
	stopChan chan struct{}
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
		stopChan: make(chan struct{}),
	}

	go rl.cleanup()

	return rl
}

func (rl *rateLimiter) Stop() {
	select {
	case <-rl.stopChan:
	default:
		close(rl.stopChan)
	}
}

func (rl *rateLimiter) cleanup() {
	ticker := time.NewTicker(rl.window * 2)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			rl.mu.Lock()
			for ip, v := range rl.visitors {
				if time.Since(v.lastReset) > rl.window*2 {
					delete(rl.visitors, ip)
				}
			}
			rl.mu.Unlock()
		case <-rl.stopChan:
			return
		}
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

	if time.Since(v.lastReset) > rl.window {
		v.tokens = rl.rate - 1
		v.lastReset = time.Now()
		return true
	}

	if v.tokens > 0 {
		v.tokens--
		return true
	}

	return false
}

// getClientIP extracts the client IP address. It splits host:port from RemoteAddr.
// If RemoteAddr is from a loopback or private network, it checks X-Forwarded-For;
// otherwise it uses RemoteAddr directly to prevent unverified header spoofing.
func getClientIP(r *http.Request) string {
	remoteIP := r.RemoteAddr
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		remoteIP = host
	}

	parsedRemote := net.ParseIP(remoteIP)
	if parsedRemote != nil && (parsedRemote.IsLoopback() || parsedRemote.IsPrivate()) {
		if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
			firstIP := strings.TrimSpace(strings.Split(forwarded, ",")[0])
			if net.ParseIP(firstIP) != nil {
				return firstIP
			}
		}
	}

	return remoteIP
}

func (rl *rateLimiter) middleware() middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting for health checks
			if r.URL.Path == "/health" {
				next.ServeHTTP(w, r)
				return
			}

			ip := getClientIP(r)
			if !rl.allow(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(http.StatusTooManyRequests)
				resp := map[string]string{
					"error": "too many requests",
				}
				if err := json.NewEncoder(w).Encode(resp); err != nil {
					slog.ErrorContext(r.Context(), "Failed to encode rate limit response", "error", err)
				}
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// rateLimit creates a rate limiting middleware.
func rateLimit(rate int, window time.Duration) middleware {
	return newRateLimiter(rate, window).middleware()
}
