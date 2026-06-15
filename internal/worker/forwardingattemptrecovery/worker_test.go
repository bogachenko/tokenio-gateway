package forwardingattemptrecovery

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	llmrequest "github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
)

type testRecoverer struct {
	calls      []int
	results    []llmrequest.ForwardingAttemptRecoveryResult
	errors     []error
	cancelFunc context.CancelFunc
}

func (r *testRecoverer) Recover(
	_ context.Context,
	batchSize int,
) (llmrequest.ForwardingAttemptRecoveryResult, error) {
	r.calls = append(r.calls, batchSize)
	index := len(r.calls) - 1
	var result llmrequest.ForwardingAttemptRecoveryResult
	if index < len(r.results) {
		result = r.results[index]
	}
	var err error
	if index < len(r.errors) {
		err = r.errors[index]
	}
	if r.cancelFunc != nil {
		r.cancelFunc()
	}
	return result, err
}

type testObserver struct {
	cycles []Cycle
}

func (o *testObserver) ObserveForwardingAttemptRecoveryCycle(
	cycle Cycle,
) {
	o.cycles = append(o.cycles, cycle)
}

type testTicker struct {
	channel chan time.Time
	stopped bool
}

func (t *testTicker) C() <-chan time.Time {
	return t.channel
}

func (t *testTicker) Stop() {
	t.stopped = true
}

func TestWorkerRunsInitialCycleBeforeTicker(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	recoverer := &testRecoverer{
		results: []llmrequest.ForwardingAttemptRecoveryResult{
			{
				Loaded:    2,
				Completed: 2,
			},
		},
		cancelFunc: cancel,
	}
	observer := &testObserver{}
	factoryCalls := 0
	worker, err := newWithTickerFactory(
		recoverer,
		observer,
		time.Minute,
		17,
		func(time.Duration) workerTicker {
			factoryCalls++
			return &testTicker{
				channel: make(chan time.Time),
			}
		},
	)
	if err != nil {
		t.Fatalf("newWithTickerFactory: %v", err)
	}

	if err := worker.Run(ctx); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !reflect.DeepEqual(
		recoverer.calls,
		[]int{17},
	) {
		t.Fatalf(
			"recover calls = %#v",
			recoverer.calls,
		)
	}
	if len(observer.cycles) != 1 ||
		observer.cycles[0].Result.Loaded != 2 ||
		observer.cycles[0].Result.Completed != 2 ||
		observer.cycles[0].Err != nil {
		t.Fatalf(
			"cycles = %#v",
			observer.cycles,
		)
	}
	if factoryCalls != 0 {
		t.Fatal(
			"ticker created after canceled initial cycle",
		)
	}
}

func TestWorkerObservesCycleErrorAndContinues(
	t *testing.T,
) {
	ctx, cancel := context.WithCancel(
		context.Background(),
	)
	cycleErr := errors.New("recover failed")
	recoverer := &testRecoverer{
		errors: []error{cycleErr, nil},
	}
	observer := &testObserver{}
	ticker := &testTicker{
		channel: make(chan time.Time, 1),
	}
	worker, err := newWithTickerFactory(
		recoverer,
		observer,
		time.Second,
		5,
		func(interval time.Duration) workerTicker {
			if interval != time.Second {
				t.Fatalf(
					"interval = %s",
					interval,
				)
			}
			return ticker
		},
	)
	if err != nil {
		t.Fatalf("newWithTickerFactory: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- worker.Run(ctx)
	}()

	for len(observer.cycles) == 0 {
		time.Sleep(time.Millisecond)
	}
	ticker.channel <- time.Now()
	for len(observer.cycles) < 2 {
		time.Sleep(time.Millisecond)
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

	if len(recoverer.calls) != 2 {
		t.Fatalf(
			"recover calls = %#v",
			recoverer.calls,
		)
	}
	if !errors.Is(
		observer.cycles[0].Err,
		cycleErr,
	) {
		t.Fatalf(
			"first cycle error = %v",
			observer.cycles[0].Err,
		)
	}
	if observer.cycles[1].Err != nil {
		t.Fatalf(
			"second cycle error = %v",
			observer.cycles[1].Err,
		)
	}
	if !ticker.stopped {
		t.Fatal("ticker was not stopped")
	}
}

func TestNewRejectsInvalidConfiguration(
	t *testing.T,
) {
	recoverer := &testRecoverer{}
	observer := &testObserver{}
	tests := []struct {
		name      string
		recoverer Recoverer
		observer  Observer
		interval  time.Duration
		batchSize int
	}{
		{
			name:      "recoverer",
			observer:  observer,
			interval:  time.Minute,
			batchSize: 1,
		},
		{
			name:      "observer",
			recoverer: recoverer,
			interval:  time.Minute,
			batchSize: 1,
		},
		{
			name:      "interval",
			recoverer: recoverer,
			observer:  observer,
			batchSize: 1,
		},
		{
			name:      "batch size",
			recoverer: recoverer,
			observer:  observer,
			interval:  time.Minute,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := New(
				test.recoverer,
				test.observer,
				test.interval,
				test.batchSize,
			)
			if !errors.Is(
				err,
				ErrInvalidWorkerConfig,
			) {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
