package auth

import (
	"sync"
	"time"
)

const (
	DefaultRateLimitEntries = 4096
	RateEntryTTL            = 30 * time.Minute
)

type LimiterClock interface{ Now() time.Time }
type limitEntry struct {
	failures     int
	last         time.Time
	blockedUntil time.Time
}
type RateLimiter struct {
	mu      sync.Mutex
	clock   LimiterClock
	max     int
	entries map[string]limitEntry
}

func NewRateLimiter(clock LimiterClock, max int) *RateLimiter {
	if max < 1 {
		max = DefaultRateLimitEntries
	}
	return &RateLimiter{clock: clock, max: max, entries: make(map[string]limitEntry)}
}
func keys(ip, username string) [2]string { return [2]string{"ip:" + ip, "user:" + username} }
func (l *RateLimiter) Allow(ip, username string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	l.cleanup(now)
	for _, key := range keys(ip, username) {
		if e, ok := l.entries[key]; ok && now.Before(e.blockedUntil) {
			return false
		}
	}
	return true
}
func (l *RateLimiter) Failure(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.clock.Now()
	l.cleanup(now)
	for _, key := range keys(ip, username) {
		if len(l.entries) >= l.max {
			l.evictOldest()
		}
		e := l.entries[key]
		if now.Sub(e.last) > RateEntryTTL {
			e.failures = 0
		}
		e.failures++
		e.last = now
		if e.failures >= 3 {
			delay := time.Second << min(e.failures-3, 8)
			e.blockedUntil = now.Add(delay)
		}
		l.entries[key] = e
	}
}
func (l *RateLimiter) Success(ip, username string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, key := range keys(ip, username) {
		delete(l.entries, key)
	}
}
func (l *RateLimiter) cleanup(now time.Time) {
	for k, e := range l.entries {
		if now.Sub(e.last) > RateEntryTTL {
			delete(l.entries, k)
		}
	}
}
func (l *RateLimiter) evictOldest() {
	var key string
	var oldest time.Time
	for k, e := range l.entries {
		if key == "" || e.last.Before(oldest) {
			key, oldest = k, e.last
		}
	}
	delete(l.entries, key)
}
func (l *RateLimiter) Size() int { l.mu.Lock(); defer l.mu.Unlock(); return len(l.entries) }
