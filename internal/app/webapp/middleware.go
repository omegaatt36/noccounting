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

// contentSecurityPolicy lets the page run only its own scripts plus Telegram's
// SDK, which Telegram requires to be loaded from telegram.org and updates in
// place, so a pinned hash would break on its next release. The few inline
// style attributes (donut gradient, bar widths) are the only inline code
// allowed; frame-ancestors admits the Telegram web clients that host Mini Apps
// in an iframe.
const contentSecurityPolicy = "default-src 'self'; " +
	"script-src 'self' https://telegram.org; " +
	"style-src 'self'; style-src-attr 'unsafe-inline'; " +
	"img-src 'self' data: blob:; " +
	"connect-src 'self'; " +
	"object-src 'none'; base-uri 'none'; form-action 'self'; " +
	"frame-ancestors 'self' https://web.telegram.org https://webk.telegram.org https://webz.telegram.org"

// securityHeaders applies the browser hardening headers to every response. The
// Mini App's init_data travels in query strings, so Referrer-Policy keeps it
// out of any Referer header.
func securityHeaders() middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			header := w.Header()
			header.Set("Content-Security-Policy", contentSecurityPolicy)
			header.Set("X-Content-Type-Options", "nosniff")
			header.Set("Referrer-Policy", "no-referrer")
			next.ServeHTTP(w, r)
		})
	}
}

// isProbe is a health or readiness check: polled every few seconds, it is
// neither worth a log line nor a share of a client's rate limit.
func isProbe(path string) bool {
	return path == "/health" || path == "/ready"
}

func logging() middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip logging for probes; they would flood the log.
			if isProbe(r.URL.Path) {
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
			// Probes and the static assets (a single page load fetches a
			// dozen of them) are not what the limit is there to guard.
			if isProbe(r.URL.Path) || strings.HasPrefix(r.URL.Path, "/static/") {
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
