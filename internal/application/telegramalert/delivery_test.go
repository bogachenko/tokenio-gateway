package telegramalert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type deliveryStoreFake struct {
	current     *domain.TelegramAlert
	findErr     error
	casErr      error
	casCalls    int
	casExpected domain.TelegramAlert
	casNext     domain.TelegramAlert
	casResult   *domain.TelegramAlert
}

func (f *deliveryStoreFake) CreateOrSuppressTelegramAlert(
	context.Context,
	domain.TelegramAlert,
	time.Duration,
) (domain.TelegramAlert, error) {
	panic("unexpected call")
}

func (f *deliveryStoreFake) FindTelegramAlertByID(
	context.Context,
	string,
) (*domain.TelegramAlert, error) {
	if f.findErr != nil {
		return nil, f.findErr
	}
	if f.current == nil {
		return nil, ports.ErrNotFound
	}
	value := *f.current
	return &value, nil
}

func (f *deliveryStoreFake) ListTelegramAlerts(
	context.Context,
	ports.TelegramAlertListFilter,
) (ports.Page[domain.TelegramAlert], error) {
	panic("unexpected call")
}

func (f *deliveryStoreFake) CompareAndSwapTelegramAlert(
	_ context.Context,
	expected domain.TelegramAlert,
	next domain.TelegramAlert,
) (domain.TelegramAlert, error) {
	f.casCalls++
	f.casExpected = expected
	f.casNext = next
	if f.casErr != nil {
		return domain.TelegramAlert{}, f.casErr
	}
	if f.casResult != nil {
		return *f.casResult, nil
	}
	return next, nil
}

type deliverySenderFake struct {
	err     error
	calls   int
	message string
}

func (f *deliverySenderFake) SendMessage(
	_ context.Context,
	message string,
) error {
	f.calls++
	f.message = message
	return f.err
}

func TestDeliveryTransitionsPendingToSent(t *testing.T) {
	now := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	alert := pendingAlert()
	store := &deliveryStoreFake{current: &alert}
	sender := &deliverySenderFake{}
	service := mustDeliveryService(t, store, sender, clockFake{now: now})

	result, err := service.Deliver(context.Background(), alert.ID)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if !result.Sent ||
		result.Alert.Status != domain.TelegramAlertStatusSent ||
		result.Alert.SentAt == nil ||
		!result.Alert.SentAt.Equal(now) {
		t.Fatalf("result = %#v", result)
	}
	if sender.calls != 1 || sender.message != alert.Message {
		t.Fatalf("sender calls=%d message=%q", sender.calls, sender.message)
	}
	if store.casCalls != 1 ||
		store.casExpected.Status != domain.TelegramAlertStatusPending ||
		store.casNext.Status != domain.TelegramAlertStatusSent {
		t.Fatalf("transition = %#v -> %#v", store.casExpected, store.casNext)
	}
}

func TestDeliveryTransitionsFailureToGenericFailedState(t *testing.T) {
	alert := pendingAlert()
	store := &deliveryStoreFake{current: &alert}
	sender := &deliverySenderFake{
		err: errors.New("secret raw transport error"),
	}
	service := mustDeliveryService(
		t,
		store,
		sender,
		clockFake{now: alert.CreatedAt.Add(time.Minute)},
	)

	result, err := service.Deliver(context.Background(), alert.ID)
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("error = %v, want delivery failed", err)
	}
	if result.Alert.Status != domain.TelegramAlertStatusFailed ||
		result.Alert.Error != DeliveryErrorUnavailable ||
		result.Alert.SentAt != nil {
		t.Fatalf("result = %#v", result)
	}
	if store.casNext.Error == sender.err.Error() {
		t.Fatal("raw sender error persisted")
	}
}

func TestDeliveryDoesNotResendNonPendingAlert(t *testing.T) {
	for _, status := range []domain.TelegramAlertStatus{
		domain.TelegramAlertStatusSent,
		domain.TelegramAlertStatusFailed,
		domain.TelegramAlertStatusSuppressed,
	} {
		t.Run(string(status), func(t *testing.T) {
			alert := pendingAlert()
			alert.Status = status
			if status == domain.TelegramAlertStatusSent {
				at := alert.CreatedAt.Add(time.Minute)
				alert.SentAt = &at
			}
			if status == domain.TelegramAlertStatusFailed {
				alert.Error = DeliveryErrorUnavailable
			}
			store := &deliveryStoreFake{current: &alert}
			sender := &deliverySenderFake{}
			service := mustDeliveryService(
				t,
				store,
				sender,
				clockFake{now: alert.CreatedAt.Add(time.Minute)},
			)

			_, err := service.Deliver(context.Background(), alert.ID)
			if !errors.Is(err, ErrAlertNotPending) {
				t.Fatalf("error = %v, want not pending", err)
			}
			if sender.calls != 0 || store.casCalls != 0 {
				t.Fatalf("sender=%d CAS=%d", sender.calls, store.casCalls)
			}
		})
	}
}

func TestDeliveryReportsUncertainStateAfterSendWhenCASFails(t *testing.T) {
	alert := pendingAlert()
	store := &deliveryStoreFake{
		current: &alert,
		casErr:  ports.ErrStoreConflict,
	}
	sender := &deliverySenderFake{}
	service := mustDeliveryService(
		t,
		store,
		sender,
		clockFake{now: alert.CreatedAt.Add(time.Minute)},
	)

	_, err := service.Deliver(context.Background(), alert.ID)
	if !errors.Is(err, ErrDeliveryStateUncertain) {
		t.Fatalf("error = %v, want uncertain", err)
	}
	if sender.calls != 1 || store.casCalls != 1 {
		t.Fatalf("sender=%d CAS=%d", sender.calls, store.casCalls)
	}
}

func TestDeliveryReportsUncertainStateWhenFailureCASFails(t *testing.T) {
	alert := pendingAlert()
	store := &deliveryStoreFake{
		current: &alert,
		casErr:  ports.ErrStoreUnavailable,
	}
	sender := &deliverySenderFake{err: errors.New("transport")}
	service := mustDeliveryService(
		t,
		store,
		sender,
		clockFake{now: alert.CreatedAt.Add(time.Minute)},
	)

	_, err := service.Deliver(context.Background(), alert.ID)
	if !errors.Is(err, ErrDeliveryStateUncertain) {
		t.Fatalf("error = %v, want uncertain", err)
	}
}

func TestDeliveryValidatesDependenciesInputAndLookup(t *testing.T) {
	alert := pendingAlert()
	store := &deliveryStoreFake{current: &alert}
	sender := &deliverySenderFake{}
	clock := clockFake{now: alert.CreatedAt.Add(time.Minute)}

	for name, build := range map[string]func() error{
		"nil store": func() error {
			_, err := NewDeliveryService(nil, sender, clock)
			return err
		},
		"nil sender": func() error {
			_, err := NewDeliveryService(store, nil, clock)
			return err
		},
		"nil clock": func() error {
			_, err := NewDeliveryService(store, sender, nil)
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := build(); !errors.Is(err, ErrDependencyRequired) {
				t.Fatalf("error = %v", err)
			}
		})
	}

	service := mustDeliveryService(t, store, sender, clock)
	if _, err := service.Deliver(nil, alert.ID); !errors.Is(
		err,
		ErrInvalidInput,
	) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := service.Deliver(
		context.Background(),
		" ",
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("blank ID error = %v", err)
	}

	missing := &deliveryStoreFake{findErr: ports.ErrNotFound}
	service = mustDeliveryService(t, missing, sender, clock)
	if _, err := service.Deliver(
		context.Background(),
		"missing",
	); !errors.Is(err, ErrAlertNotFound) {
		t.Fatalf("missing error = %v", err)
	}

	failed := &deliveryStoreFake{findErr: ports.ErrStoreUnavailable}
	service = mustDeliveryService(t, failed, sender, clock)
	if _, err := service.Deliver(
		context.Background(),
		"alert-1",
	); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("store error = %v", err)
	}
}

func pendingAlert() domain.TelegramAlert {
	return domain.TelegramAlert{
		ID:         "tgalt_1",
		AlertType:  AlertTypeResellerBalanceLow,
		DedupeKey:  "reseller-1",
		ResellerID: "reseller-1",
		Message:    "low balance",
		Status:     domain.TelegramAlertStatusPending,
		CreatedAt:  time.Date(2026, 6, 15, 9, 0, 0, 0, time.UTC),
	}
}

func mustDeliveryService(
	t *testing.T,
	store ports.TelegramAlertStore,
	sender MessageSender,
	clock ports.Clock,
) *DeliveryService {
	t.Helper()
	service, err := NewDeliveryService(store, sender, clock)
	if err != nil {
		t.Fatalf("NewDeliveryService: %v", err)
	}
	return service
}

var _ ports.TelegramAlertStore = (*deliveryStoreFake)(nil)
var _ MessageSender = (*deliverySenderFake)(nil)
