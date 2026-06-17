package app

import (
	"context"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
)

type contextRetryWaiter struct{}

func (contextRetryWaiter) Wait(
	ctx context.Context,
	delay time.Duration,
) error {
	if ctx == nil || delay < 0 {
		return llmrequest.ErrInvalidInput
	}
	if delay == 0 {
		return ctx.Err()
	}

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
