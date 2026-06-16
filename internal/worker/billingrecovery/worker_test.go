package billingrecovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
)

type recoveryWorkerFake struct {
	calls []int
	err   error
}

func (f *recoveryWorkerFake) RunCycle(
	_ context.Context,
	limit int,
) (billingapp.RecoveryCycleResult, error) {
	f.calls = append(f.calls, limit)
	return billingapp.RecoveryCycleResult{
		DiscoveredBatchIDs: []string{"billchg_1"},
		ProcessedBatchIDs:  []string{"billchg_1"},
	}, f.err
}

type recoveryObserverFake struct {
	cycles []Cycle
}

func (f *recoveryObserverFake) ObserveBillingRecoveryCycle(cycle Cycle) {
	f.cycles = append(f.cycles, cycle)
}

type fakeTicker struct {
	channel chan time.Time
	stopped bool
}

func (t *fakeTicker) C() <-chan time.Time {
	return t.channel
}

func (t *fakeTicker) Stop() {
	t.stopped = true
}

func TestWorkerRunsInitialCycleBeforeTickerAndContinuesAfterError(t *testing.T) {
	recoverer := &recoveryWorkerFake{
		err: errors.New("cycle failed"),
	}
	observer := &recoveryObserverFake{}
	ticker := &fakeTicker{
		channel: make(chan time.Time, 1),
	}
	worker, err := newWithTickerFactory(
		recoverer,
		observer,
		time.Minute,
		17,
		func(time.Duration) workerTicker {
			return ticker
		},
	)
	if err != nil {
		t.Fatalf("newWithTickerFactory: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- worker.Run(ctx)
	}()

	deadline := time.After(time.Second)
	for len(recoverer.calls) < 1 {
		select {
		case <-deadline:
			t.Fatal("initial cycle was not run")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if !reflect.DeepEqual(recoverer.calls, []int{17}) {
		t.Fatalf("initial calls=%v", recoverer.calls)
	}

	ticker.channel <- time.Now()
	deadline = time.After(time.Second)
	for len(recoverer.calls) < 2 {
		select {
		case <-deadline:
			t.Fatal("ticker cycle was not run")
		default:
			time.Sleep(time.Millisecond)
		}
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("worker did not stop")
	}

	if !ticker.stopped {
		t.Fatal("ticker was not stopped")
	}
	if len(observer.cycles) != 2 {
		t.Fatalf("observed cycles=%d, want 2", len(observer.cycles))
	}
	for _, cycle := range observer.cycles {
		if !errors.Is(cycle.Err, recoverer.err) {
			t.Fatalf("cycle error=%v, want %v", cycle.Err, recoverer.err)
		}
	}
}
