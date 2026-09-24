package app

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter is a fixed-window-per-key counter kept in memory.
// ponytail: resets on restart and is per-process; fine for one container.
type limiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
	now    func() time.Time
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{hits: map[string][]time.Time{}, limit: limit, window: window, now: time.Now}
}

// prune drops expired hits for key and returns the live ones. Caller holds mu.
func (l *limiter) prune(key string, now time.Time) []time.Time {
	cutoff := now.Add(-l.window)
	kept := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	l.hits[key] = kept
	if len(l.hits) > 10000 { // ponytail: crude sweep; a real LRU if this ever matters
		for k, v := range l.hits {
			if len(v) == 0 || !v[len(v)-1].After(cutoff) {
				delete(l.hits, k)
			}
		}
	}
	return kept
}

// Blocked reports whether key has reached the limit without recording anything.
func (l *limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, l.now())) >= l.limit
}

// Hit records one event for key (used to count only failed logins).
func (l *limiter) Hit(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.hits[key] = append(l.prune(key, now), now)
}

// Allow records a hit if under the limit and reports whether it was allowed.
func (l *limiter) Allow(key string) bool {
	if l.Blocked(key) {
		return false
	}
	l.Hit(key)
	return true
}

// clientIP trusts the first X-Forwarded-For entry because the app always sits
// behind Coolify's Traefik. ponytail: spoofable if ever exposed directly.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		return strings.TrimSpace(strings.Split(xff, ",")[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
