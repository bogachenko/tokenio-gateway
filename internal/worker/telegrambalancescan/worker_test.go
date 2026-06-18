package telegrambalancescan

import (
	"context"
	"errors"
	"testing"
	"time"

	telegramalert "github.com/bogachenko/tokenio-gateway/internal/application/telegramalert"
)

type scannerFake struct {
	results []telegramalert.BalanceScanResult
	errs    []error
	calls   []int
}

func (f *scannerFake) ScanEnabledResellers(
	_ context.Context,
	limit int,
	offset int,
) (telegramalert.BalanceScanResult, error) {
	f.calls = append(f.calls, offset)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		if err != nil {
			return telegramalert.BalanceScanResult{}, err
		}
	}
	if len(f.results) == 0 {
		return telegramalert.BalanceScanResult{
			Selected:   0,
			NextOffset: offset,
			Finished:   true,
		}, nil
	}
	result := f.results[0]
	f.results = f.results[1:]
	return result, nil
}

type observerFake struct {
	cycles []Cycle
}

func (f *observerFake) ObserveTelegramBalanceScanCycle(cycle Cycle) {
	f.cycles = append(f.cycles, cycle)
}

func TestWorkerAdvancesOffsetAndResetsAtEnd(t *testing.T) {
	scanner := &scannerFake{
		results: []telegramalert.BalanceScanResult{
			{Selected: 2, NextOffset: 2, Finished: false},
			{Selected: 1, NextOffset: 3, Finished: true},
		},
	}
	observer := &observerFake{}
	worker, err := New(scanner, observer, time.Minute, 2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	worker.runCycle(context.Background())
	worker.runCycle(context.Background())
	worker.runCycle(context.Background())

	if len(scanner.calls) != 3 ||
		scanner.calls[0] != 0 ||
		scanner.calls[1] != 2 ||
		scanner.calls[2] != 0 {
		t.Fatalf("calls = %#v", scanner.calls)
	}
	if len(observer.cycles) != 3 {
		t.Fatalf("cycles = %d, want 3", len(observer.cycles))
	}
}

func TestWorkerKeepsOffsetAfterScanError(t *testing.T) {
	scanner := &scannerFake{
		results: []telegramalert.BalanceScanResult{
			{Selected: 2, NextOffset: 2, Finished: false},
		},
		errs: []error{nil, errors.New("store down"), nil},
	}
	observer := &observerFake{}
	worker, err := New(scanner, observer, time.Minute, 2)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	worker.runCycle(context.Background())
	worker.runCycle(context.Background())
	worker.runCycle(context.Background())

	if len(scanner.calls) != 3 ||
		scanner.calls[0] != 0 ||
		scanner.calls[1] != 2 ||
		scanner.calls[2] != 2 {
		t.Fatalf("calls = %#v", scanner.calls)
	}
	if len(observer.cycles) != 3 ||
		observer.cycles[1].Err == nil {
		t.Fatalf("cycles = %#v", observer.cycles)
	}
}
