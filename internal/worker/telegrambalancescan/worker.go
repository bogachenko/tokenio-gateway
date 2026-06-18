package telegrambalancescan

import (
	"context"
	"errors"
	"time"

	telegramalert "github.com/bogachenko/tokenio-gateway/internal/application/telegramalert"
)

var ErrInvalidWorkerConfig = errors.New(
	"invalid Telegram balance-scan worker config",
)

type Scanner interface {
	ScanEnabledResellers(
		context.Context,
		int,
		int,
	) (telegramalert.BalanceScanResult, error)
}

type Cycle struct {
	Result telegramalert.BalanceScanResult
	Err    error
}

type Observer interface {
	ObserveTelegramBalanceScanCycle(Cycle)
}

type Worker struct {
	scanner   Scanner
	observer  Observer
	interval  time.Duration
	batchSize int
	offset    int
	newTicker tickerFactory
}

func New(
	scanner Scanner,
	observer Observer,
	interval time.Duration,
	batchSize int,
) (*Worker, error) {
	return newWithTickerFactory(
		scanner,
		observer,
		interval,
		batchSize,
		newRealTicker,
	)
}

func newWithTickerFactory(
	scanner Scanner,
	observer Observer,
	interval time.Duration,
	batchSize int,
	factory tickerFactory,
) (*Worker, error) {
	if scanner == nil ||
		observer == nil ||
		interval <= 0 ||
		batchSize <= 0 ||
		factory == nil {
		return nil, ErrInvalidWorkerConfig
	}
	return &Worker{
		scanner:   scanner,
		observer:  observer,
		interval:  interval,
		batchSize: batchSize,
		newTicker: factory,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	if ctx == nil ||
		w == nil ||
		w.scanner == nil ||
		w.observer == nil ||
		w.interval <= 0 ||
		w.batchSize <= 0 ||
		w.newTicker == nil {
		return ErrInvalidWorkerConfig
	}
	if ctx.Err() != nil {
		return nil
	}

	w.runCycle(ctx)
	if ctx.Err() != nil {
		return nil
	}

	ticker := w.newTicker(w.interval)
	if ticker == nil {
		return ErrInvalidWorkerConfig
	}
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C():
			w.runCycle(ctx)
		}
	}
}

func (w *Worker) runCycle(ctx context.Context) {
	result, err := w.scanner.ScanEnabledResellers(
		ctx,
		w.batchSize,
		w.offset,
	)
	if err == nil {
		if result.Finished {
			w.offset = 0
		} else {
			w.offset = result.NextOffset
		}
	}
	w.observer.ObserveTelegramBalanceScanCycle(Cycle{
		Result: result,
		Err:    err,
	})
}

type workerTicker interface {
	C() <-chan time.Time
	Stop()
}

type tickerFactory func(time.Duration) workerTicker

type realTicker struct {
	ticker *time.Ticker
}

func newRealTicker(interval time.Duration) workerTicker {
	return &realTicker{ticker: time.NewTicker(interval)}
}

func (t *realTicker) C() <-chan time.Time {
	return t.ticker.C
}

func (t *realTicker) Stop() {
	t.ticker.Stop()
}
