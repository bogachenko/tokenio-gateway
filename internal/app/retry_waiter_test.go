package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
)

func TestContextRetryWaiterHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := (contextRetryWaiter{}).Wait(ctx, time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestContextRetryWaiterRejectsInvalidInput(t *testing.T) {
	waiter := contextRetryWaiter{}

	if !errors.Is(
		waiter.Wait(nil, time.Second),
		llmrequest.ErrInvalidInput,
	) {
		t.Fatal("nil context was accepted")
	}
	if !errors.Is(
		waiter.Wait(context.Background(), -time.Second),
		llmrequest.ErrInvalidInput,
	) {
		t.Fatal("negative delay was accepted")
	}
}
