package tokens

import (
	"sync"
	"time"
)

type RateLimiter struct {
	maxRequests int
	window      time.Duration
	now         func() time.Time

	mu       sync.Mutex
	requests map[string][]time.Time
}

func NewRateLimiter(maxRequests int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		maxRequests: maxRequests,
		window:      window,
		now:         time.Now,
		requests:    make(map[string][]time.Time),
	}
}

func (rl *RateLimiter) IsAllowed(clientID string) bool {
	now := rl.now()
	cutoff := now.Add(-rl.window)

	rl.mu.Lock()
	defer rl.mu.Unlock()

	kept := rl.requests[clientID][:0]
	for _, t := range rl.requests[clientID] {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= rl.maxRequests {
		rl.requests[clientID] = kept
		return false
	}
	rl.requests[clientID] = append(kept, now)
	return true
}

func (rl *RateLimiter) RetryAfter(clientID string) int {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	reqs := rl.requests[clientID]
	if len(reqs) == 0 {
		return 0
	}
	oldest := reqs[0]
	for _, t := range reqs[1:] {
		if t.Before(oldest) {
			oldest = t
		}
	}
	remaining := rl.window - rl.now().Sub(oldest)
	if remaining < 0 {
		return 0
	}
	return int(remaining.Seconds())
}

func (rl *RateLimiter) Remove(clientID string) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	delete(rl.requests, clientID)
}
