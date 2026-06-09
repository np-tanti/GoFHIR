package gatekeeper

import (
	"sync"
	"testing"

	"golang.org/x/time/rate"
)

func TestRateLimiterAllows(t *testing.T) {
	rl := NewRateLimiter(rate.Limit(100), 10)

	for i := 0; i < 10; i++ {
		if !rl.Allow("10.0.0.1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
}

func TestRateLimiterBlocks(t *testing.T) {
	rl := NewRateLimiter(rate.Limit(1), 1)

	if !rl.Allow("10.0.0.1") {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow("10.0.0.1") {
		t.Fatal("second request should be blocked (burst=1, rate=1/s)")
	}
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	rl := NewRateLimiter(rate.Limit(1), 1)

	if !rl.Allow("10.0.0.1") {
		t.Fatal("first IP should be allowed")
	}
	if !rl.Allow("10.0.0.2") {
		t.Fatal("second IP should be allowed (separate limiter)")
	}
	if rl.Allow("10.0.0.1") {
		t.Fatal("first IP should be blocked")
	}
}

func TestRateLimiterCleanup(t *testing.T) {
	rl := NewRateLimiter(rate.Limit(100), 10)

	ips := []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"}
	for _, ip := range ips {
		rl.Allow(ip)
	}

	rl.mu.RLock()
	count := len(rl.visitors)
	rl.mu.RUnlock()
	if count != 3 {
		t.Fatalf("expected 3 visitors, got %d", count)
	}

	rl.Cleanup()

	rl.mu.RLock()
	count = len(rl.visitors)
	rl.mu.RUnlock()
	if count != 0 {
		t.Fatalf("expected 0 visitors after cleanup, got %d", count)
	}
}

func TestRateLimiterSetRate(t *testing.T) {
	rl := NewRateLimiter(rate.Limit(100), 50)

	rl.Allow("10.0.0.1")

	rl.SetRate(rate.Limit(10), 5)

	if rl.rate != 10 || rl.burst != 5 {
		t.Errorf("rate=%v burst=%d after SetRate", rl.rate, rl.burst)
	}
}

func TestRateLimiterConcurrent(t *testing.T) {
	rl := NewRateLimiter(rate.Limit(1000), 100)
	var wg sync.WaitGroup
	n := 50

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				rl.Allow("10.0.0.1")
			}
		}()
	}
	wg.Wait()

	rl.mu.RLock()
	_, exists := rl.visitors["10.0.0.1"]
	rl.mu.RUnlock()
	if !exists {
		t.Fatal("visitor should exist after concurrent access")
	}
}

func TestRateLimiterHighBurst(t *testing.T) {
	burst := 100
	rl := NewRateLimiter(rate.Limit(1), burst)

	for i := 0; i < burst; i++ {
		if !rl.Allow("10.0.0.1") {
			t.Fatalf("burst request %d should be allowed", i+1)
		}
	}
	if rl.Allow("10.0.0.1") {
		t.Fatal("request beyond burst should be blocked")
	}
}