package telegramdelivery

import (
	"context"
	"errors"
	"testing"
	"time"

	telegramalert "github.com/bogachenko/tokenio-gateway/internal/application/telegramalert"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type alertListerFake struct {
	page   ports.Page[domain.TelegramAlert]
	err    error
	filter ports.TelegramAlertListFilter
}

func (f *alertListerFake) ListTelegramAlerts(
	_ context.Context,
	filter ports.TelegramAlertListFilter,
) (ports.Page[domain.TelegramAlert], error) {
	f.filter = filter
	if f.err != nil {
		return ports.Page[domain.TelegramAlert]{}, f.err
	}
	return f.page, nil
}

type delivererFake struct {
	errByID  map[string]error
	sentByID map[string]bool
	calls    []string
}

func (f *delivererFake) Deliver(
	_ context.Context,
	alertID string,
) (telegramalert.DeliveryResult, error) {
	f.calls = append(f.calls, alertID)
	if err := f.errByID[alertID]; err != nil {
		return telegramalert.DeliveryResult{}, err
	}
	return telegramalert.DeliveryResult{Sent: f.sentByID[alertID]}, nil
}

type observerFake struct {
	cycles []Cycle
}

func (f *observerFake) ObserveTelegramDeliveryCycle(cycle Cycle) {
	f.cycles = append(f.cycles, cycle)
}

func TestWorkerDeliversPendingAlerts(t *testing.T) {
	lister := &alertListerFake{
		page: ports.Page[domain.TelegramAlert]{
			Items: []domain.TelegramAlert{
				{ID: "a1", Status: domain.TelegramAlertStatusPending},
				{ID: "a2", Status: domain.TelegramAlertStatusPending},
				{ID: "sent", Status: domain.TelegramAlertStatusSent},
			},
			Total: 3,
		},
	}
	deliverer := &delivererFake{
		errByID: map[string]error{
			"a2": telegramalert.ErrDeliveryFailed,
		},
		sentByID: map[string]bool{
			"a1": true,
		},
	}
	observer := &observerFake{}
	worker, err := New(lister, deliverer, observer, time.Minute, 10)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	worker.runCycle(context.Background())

	if lister.filter.Status != domain.TelegramAlertStatusPending ||
		lister.filter.Page.Limit != 10 ||
		lister.filter.Page.Offset != 0 {
		t.Fatalf("filter = %#v", lister.filter)
	}
	if len(deliverer.calls) != 2 ||
		deliverer.calls[0] != "a1" ||
		deliverer.calls[1] != "a2" {
		t.Fatalf("deliver calls = %#v", deliverer.calls)
	}
	if len(observer.cycles) != 1 {
		t.Fatalf("cycles = %d, want 1", len(observer.cycles))
	}
	result := observer.cycles[0].Result
	if result.Selected != 3 ||
		result.Delivered != 1 ||
		result.Failed != 1 ||
		result.Skipped != 1 ||
		result.Uncertain != 0 ||
		observer.cycles[0].Err != nil {
		t.Fatalf("cycle = %#v", observer.cycles[0])
	}
}

func TestWorkerReportsListError(t *testing.T) {
	observer := &observerFake{}
	worker, err := New(
		&alertListerFake{err: errors.New("store down")},
		&delivererFake{},
		observer,
		time.Minute,
		10,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	worker.runCycle(context.Background())

	if len(observer.cycles) != 1 ||
		observer.cycles[0].Err == nil {
		t.Fatalf("cycles = %#v", observer.cycles)
	}
}
