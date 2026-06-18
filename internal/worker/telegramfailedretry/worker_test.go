package telegramfailedretry

import (
	"context"
	"errors"
	"testing"
	"time"

	telegramalert "github.com/bogachenko/tokenio-gateway/internal/application/telegramalert"
)

type recovererFake struct {
	result telegramalert.RecoveryResult
	err    error
	limit  int
	calls  int
}

func (f *recovererFake) RecoverFailed(
	_ context.Context,
	limit int,
) (telegramalert.RecoveryResult, error) {
	f.calls++
	f.limit = limit
	if f.err != nil {
		return telegramalert.RecoveryResult{}, f.err
	}
	return f.result, nil
}

type observerFake struct {
	cycles []Cycle
}

func (f *observerFake) ObserveTelegramFailedRetryCycle(cycle Cycle) {
	f.cycles = append(f.cycles, cycle)
}

func TestWorkerRunsRecoveryCycle(t *testing.T) {
	recoverer := &recovererFake{
		result: telegramalert.RecoveryResult{
			Selected: 2,
			Retried:  2,
			Sent:     1,
		},
	}
	observer := &observerFake{}
	worker, err := New(recoverer, observer, time.Minute, 25)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	worker.runCycle(context.Background())

	if recoverer.calls != 1 || recoverer.limit != 25 {
		t.Fatalf("calls=%d limit=%d", recoverer.calls, recoverer.limit)
	}
	if len(observer.cycles) != 1 {
		t.Fatalf("cycles=%d, want 1", len(observer.cycles))
	}
	if observer.cycles[0].Err != nil ||
		observer.cycles[0].Result.Selected != 2 ||
		observer.cycles[0].Result.Retried != 2 ||
		observer.cycles[0].Result.Sent != 1 {
		t.Fatalf("cycle=%#v", observer.cycles[0])
	}
}

func TestWorkerReportsRecoveryError(t *testing.T) {
	recoverer := &recovererFake{err: errors.New("store down")}
	observer := &observerFake{}
	worker, err := New(recoverer, observer, time.Minute, 25)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	worker.runCycle(context.Background())

	if recoverer.calls != 1 || recoverer.limit != 25 {
		t.Fatalf("calls=%d limit=%d", recoverer.calls, recoverer.limit)
	}
	if len(observer.cycles) != 1 ||
		observer.cycles[0].Err == nil {
		t.Fatalf("cycles=%#v", observer.cycles)
	}
}
