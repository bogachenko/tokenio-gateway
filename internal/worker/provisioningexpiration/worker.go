package provisioningexpiration

import (
	"context"
	"errors"
	"time"

	provisioningapp "github.com/bogachenko/tokenio-gateway/internal/application/provisioning"
)

var ErrInvalidWorkerConfig = errors.New(
	"invalid provisioning expiration worker config",
)

type Expirer interface {
	ExpireDue(
		context.Context,
		int,
	) (provisioningapp.ExpireDueResult, error)
}

type Cycle struct {
	Result provisioningapp.ExpireDueResult
	Err    error
}

type Observer interface {
	ObserveProvisioningExpirationCycle(Cycle)
}

type Worker struct {
	expirer   Expirer
	observer  Observer
	interval  time.Duration
	batchSize int
	newTicker tickerFactory
}

func New(
	expirer Expirer,
	observer Observer,
	interval time.Duration,
	batchSize int,
) (*Worker, error) {
	return newWithTickerFactory(
		expirer,
		observer,
		interval,
		batchSize,
		newRealTicker,
	)
}

func newWithTickerFactory(
	expirer Expirer,
	observer Observer,
	interval time.Duration,
	batchSize int,
	factory tickerFactory,
) (*Worker, error) {
	if expirer == nil ||
		observer == nil ||
		interval <= 0 ||
		batchSize <= 0 ||
		factory == nil {
		return nil, ErrInvalidWorkerConfig
	}

	return &Worker{
		expirer:   expirer,
		observer:  observer,
		interval:  interval,
		batchSize: batchSize,
		newTicker: factory,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	if ctx == nil ||
		w == nil ||
		w.expirer == nil ||
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
	result, err := w.expirer.ExpireDue(
		ctx,
		w.batchSize,
	)
	w.observer.ObserveProvisioningExpirationCycle(
		Cycle{
			Result: result,
			Err:    err,
		},
	)
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
	return &realTicker{
		ticker: time.NewTicker(interval),
	}
}

func (t *realTicker) C() <-chan time.Time {
	return t.ticker.C
}

func (t *realTicker) Stop() {
	t.ticker.Stop()
}
