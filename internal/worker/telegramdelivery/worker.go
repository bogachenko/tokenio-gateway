package telegramdelivery

import (
	"context"
	"errors"
	"time"

	telegramalert "github.com/bogachenko/tokenio-gateway/internal/application/telegramalert"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var ErrInvalidWorkerConfig = errors.New(
	"invalid Telegram delivery worker config",
)

type AlertLister interface {
	ListTelegramAlerts(
		context.Context,
		ports.TelegramAlertListFilter,
	) (ports.Page[domain.TelegramAlert], error)
}

type Deliverer interface {
	Deliver(context.Context, string) (telegramalert.DeliveryResult, error)
}

type CycleResult struct {
	Selected  int
	Delivered int
	Failed    int
	Uncertain int
	Skipped   int
}

type Cycle struct {
	Result CycleResult
	Err    error
}

type Observer interface {
	ObserveTelegramDeliveryCycle(Cycle)
}

type Worker struct {
	alerts    AlertLister
	deliverer Deliverer
	observer  Observer
	interval  time.Duration
	batchSize int
	newTicker tickerFactory
}

func New(
	alerts AlertLister,
	deliverer Deliverer,
	observer Observer,
	interval time.Duration,
	batchSize int,
) (*Worker, error) {
	return newWithTickerFactory(
		alerts,
		deliverer,
		observer,
		interval,
		batchSize,
		newRealTicker,
	)
}

func newWithTickerFactory(
	alerts AlertLister,
	deliverer Deliverer,
	observer Observer,
	interval time.Duration,
	batchSize int,
	factory tickerFactory,
) (*Worker, error) {
	if alerts == nil ||
		deliverer == nil ||
		observer == nil ||
		interval <= 0 ||
		batchSize <= 0 ||
		factory == nil {
		return nil, ErrInvalidWorkerConfig
	}
	return &Worker{
		alerts:    alerts,
		deliverer: deliverer,
		observer:  observer,
		interval:  interval,
		batchSize: batchSize,
		newTicker: factory,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	if ctx == nil ||
		w == nil ||
		w.alerts == nil ||
		w.deliverer == nil ||
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
	result, err := w.deliverPending(ctx)
	w.observer.ObserveTelegramDeliveryCycle(Cycle{
		Result: result,
		Err:    err,
	})
}

func (w *Worker) deliverPending(
	ctx context.Context,
) (CycleResult, error) {
	page, err := w.alerts.ListTelegramAlerts(
		ctx,
		ports.TelegramAlertListFilter{
			Status: domain.TelegramAlertStatusPending,
			Page: ports.PageRequest{
				Limit:  w.batchSize,
				Offset: 0,
			},
		},
	)
	if err != nil {
		return CycleResult{}, err
	}
	result := CycleResult{
		Selected: len(page.Items),
	}
	if len(page.Items) > w.batchSize {
		return result, ErrInvalidWorkerConfig
	}

	for _, alert := range page.Items {
		if alert.ID == "" ||
			alert.Status != domain.TelegramAlertStatusPending {
			result.Skipped++
			continue
		}
		delivery, err := w.deliverer.Deliver(ctx, alert.ID)
		switch {
		case err == nil && delivery.Sent:
			result.Delivered++
		case errors.Is(err, telegramalert.ErrDeliveryFailed):
			result.Failed++
		default:
			result.Uncertain++
		}
	}
	return result, nil
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
