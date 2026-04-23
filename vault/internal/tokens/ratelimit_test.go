package tokens

import (
	"sync"
	"testing"
	"time"
)

func TestRateLimiterAllowsUnderLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)
	for i := 0; i < 3; i++ {
		if !rl.IsAllowed("u") {
			t.Fatalf("request %d denied, want allowed", i+1)
		}
	}
}

func TestRateLimiterDeniesOverLimit(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)
	rl.IsAllowed("u")
	rl.IsAllowed("u")
	if rl.IsAllowed("u") {
		t.Error("3rd request allowed, want denied")
	}
}

func TestRateLimiterPerClient(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	if !rl.IsAllowed("a") {
		t.Fatal("a denied")
	}
	if !rl.IsAllowed("b") {
		t.Fatal("b denied — should be tracked separately")
	}
	if rl.IsAllowed("a") {
		t.Error("a 2nd request allowed, want denied")
	}
}

func TestRateLimiterRetryAfter(t *testing.T) {
	rl := NewRateLimiter(1, 10*time.Second)
	now := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)
	rl.now = func() time.Time { return now }
	rl.IsAllowed("u")
	rl.now = func() time.Time { return now.Add(3 * time.Second) }
	got := rl.RetryAfter("u")
	if got < 6 || got > 7 {
		t.Errorf("RetryAfter = %d, want ~7", got)
	}
}

func TestRateLimiterRemove(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)
	rl.IsAllowed("u")
	rl.Remove("u")
	if !rl.IsAllowed("u") {
		t.Error("after Remove, request should be allowed again")
	}
}

func TestRateLimiterConcurrent(t *testing.T) {
	rl := NewRateLimiter(100, time.Minute)
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			rl.IsAllowed("u")
		}()
	}
	wg.Wait()
}

func TestRateLimiterWindowEvicts(t *testing.T) {
	rl := NewRateLimiter(1, 5*time.Second)
	now := time.Date(2026, 4, 23, 0, 0, 0, 0, time.UTC)
	rl.now = func() time.Time { return now }
	if !rl.IsAllowed("u") {
		t.Fatal("first denied")
	}
	if rl.IsAllowed("u") {
		t.Fatal("second allowed inside window")
	}
	rl.now = func() time.Time { return now.Add(6 * time.Second) }
	if !rl.IsAllowed("u") {
		t.Error("after window, request should be allowed")
	}
}
