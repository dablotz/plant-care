package middleware

import (
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// SecurityHeaders adds standard security response headers to every response.
func SecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Content-Security-Policy",
			"default-src 'self'; img-src 'self' data: blob:; style-src 'self' 'unsafe-inline'; script-src 'self'")
		next.ServeHTTP(w, r)
	})
}

// NewBearerAuth returns middleware that requires a valid Authorization: Bearer <token>
// header on every request. The token is read from the API_KEY environment variable at
// construction time. If API_KEY is not set, auth is disabled and a warning is logged
// (dev mode).
func NewBearerAuth(logger *slog.Logger) func(http.Handler) http.Handler {
	apiKey := os.Getenv("API_KEY")
	if apiKey == "" {
		logger.Warn("API_KEY not set — authentication disabled (dev mode)")
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if apiKey == "" {
				next.ServeHTTP(w, r)
				return
			}
			token, found := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			if !found || token != apiKey {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusUnauthorized)
				w.Write([]byte(`{"error":"unauthorized"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewRateLimiter returns middleware enforcing per-IP rate limits using a token bucket.
// rps is the sustained requests per second; burst is the maximum burst size.
// Stale entries (not seen in 10 minutes) are cleaned up automatically every 5 minutes.
func NewRateLimiter(logger *slog.Logger, rps float64, burst int) func(http.Handler) http.Handler {
	var mu sync.Mutex
	limiters := make(map[netip.Addr]*ipLimiter)

	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			mu.Lock()
			for ip, l := range limiters {
				if time.Since(l.lastSeen) > 10*time.Minute {
					delete(limiters, ip)
				}
			}
			mu.Unlock()
		}
	}()

	getLimiter := func(ip netip.Addr) *rate.Limiter {
		mu.Lock()
		defer mu.Unlock()
		l, ok := limiters[ip]
		if !ok {
			l = &ipLimiter{limiter: rate.NewLimiter(rate.Limit(rps), burst)}
			limiters[ip] = l
		}
		l.lastSeen = time.Now()
		return l.limiter
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			addrPort, err := netip.ParseAddrPort(r.RemoteAddr)
			if err != nil {
				logger.Warn("rate limiter: could not parse remote addr", "addr", r.RemoteAddr)
				next.ServeHTTP(w, r)
				return
			}
			if !getLimiter(addrPort.Addr()).Allow() {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				w.Write([]byte(`{"error":"rate limit exceeded"}`))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
