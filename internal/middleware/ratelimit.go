package middleware

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"portfolio/cmd/templates"

	"github.com/a-h/templ"
)

type clientStats struct {
	timestamps []time.Time
}

type RateLimiter struct {
	mu      sync.Mutex
	clients map[string]*clientStats
	limit   int
	window  time.Duration
}

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	rl := &RateLimiter{
		clients: make(map[string]*clientStats),
		limit:   limit,
		window:  window,
	}

	go func() {
		for {
			time.Sleep(5 * time.Minute)
			rl.cleanup()
		}
	}()

	return rl
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, client := range rl.clients {
		var valid []time.Time
		for _, ts := range client.timestamps {
			if now.Sub(ts) <= rl.window {
				valid = append(valid, ts)
			}
		}
		if len(valid) == 0 {
			delete(rl.clients, ip)
		} else {
			client.timestamps = valid
		}
	}
}

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

func (rl *RateLimiter) Limit(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := getClientIP(r)

		rl.mu.Lock()
		client, exists := rl.clients[ip]
		if !exists {
			client = &clientStats{}
			rl.clients[ip] = client
		}

		now := time.Now()

		var valid []time.Time
		for _, ts := range client.timestamps {
			if now.Sub(ts) <= rl.window {
				valid = append(valid, ts)
			}
		}

		if len(valid) >= rl.limit {
			rl.mu.Unlock()
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(rl.window.Seconds())))
			w.WriteHeader(http.StatusTooManyRequests)
			templ.Handler(templates.ErrorLoginIn("Too many login attempts. Please wait 1 minute before trying again.")).ServeHTTP(w, r)
			return
		}

		client.timestamps = append(valid, now)
		rl.mu.Unlock()

		next.ServeHTTP(w, r)
	})
}
