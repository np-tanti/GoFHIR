package gatekeeper

import (
	"sync"

	"golang.org/x/time/rate"
)

type RateLimiter struct {
	mu       sync.RWMutex
	visitors map[string]*rate.Limiter
	rate     rate.Limit
	burst    int
}

func NewRateLimiter(r rate.Limit, burst int) *RateLimiter {
	return &RateLimiter{
		visitors: make(map[string]*rate.Limiter),
		rate:     r,
		burst:    burst,
	}
}

func (rl *RateLimiter) Allow(ip string) bool {
	rl.mu.RLock()
	limiter, ok := rl.visitors[ip]
	rl.mu.RUnlock()
	if !ok {
		limiter = rate.NewLimiter(rl.rate, rl.burst)
		rl.mu.Lock()
		rl.visitors[ip] = limiter
		rl.mu.Unlock()
	}
	return limiter.Allow()
}

func (rl *RateLimiter) SetRate(r rate.Limit, burst int) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.rate = r
	rl.burst = burst
}

func (rl *RateLimiter) Cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	rl.visitors = make(map[string]*rate.Limiter)
}
