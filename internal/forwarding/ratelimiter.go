package forwarding

import (
	"context"
	"fmt"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

// PURPOSE: local in-memory per-model outbound route limiter with bounded blocking waits.
type RateLimiter struct {
	maxWait  time.Duration
	limit    rate.Limit
	burst    int
	disabled bool

	mu       sync.Mutex
	limiters map[string]*rate.Limiter
}

type RateLimitWaitTimeoutError struct {
	Model   string
	MaxWait time.Duration
}

func (e *RateLimitWaitTimeoutError) Error() string {
	return fmt.Sprintf("upstream route rate limit wait timeout for model %q after %s", e.Model, e.MaxWait)
}

func NewRateLimiter(rpm int, burst int, maxWait time.Duration) *RateLimiter {
	if rpm <= 0 || burst <= 0 {
		return &RateLimiter{
			maxWait:  maxWait,
			disabled: true,
			limiters: map[string]*rate.Limiter{},
		}
	}
	return &RateLimiter{
		maxWait:  maxWait,
		limit:    rate.Every(time.Minute / time.Duration(rpm)),
		burst:    burst,
		limiters: map[string]*rate.Limiter{},
	}
}

func (r *RateLimiter) Wait(ctx context.Context, model string) error {
	if r == nil || r.disabled {
		return nil
	}
	if model == "" {
		model = "default"
	}
	limiter := r.get(model)

	reservation := limiter.Reserve()
	if !reservation.OK() {
		return &RateLimitWaitTimeoutError{Model: model, MaxWait: r.maxWait}
	}
	delay := reservation.Delay()
	if delay > r.maxWait {
		reservation.Cancel()
		return &RateLimitWaitTimeoutError{Model: model, MaxWait: r.maxWait}
	}
	if delay <= 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		reservation.Cancel()
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (r *RateLimiter) get(model string) *rate.Limiter {
	r.mu.Lock()
	defer r.mu.Unlock()
	limiter, ok := r.limiters[model]
	if !ok {
		limiter = rate.NewLimiter(r.limit, r.burst)
		r.limiters[model] = limiter
	}
	return limiter
}
