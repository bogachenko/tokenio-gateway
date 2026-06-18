package telegramalert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type recoveryStoreFake struct {
	page       ports.Page[domain.TelegramAlert]
	listErr    error
	casErrByID map[string]error
	casCalls   []recoveryCASTransition
	filter     ports.TelegramAlertListFilter
}

type recoveryCASTransition struct {
	expected domain.TelegramAlert
	next     domain.TelegramAlert
}

func (f *recoveryStoreFake) CreateOrSuppressTelegramAlert(
	context.Context,
	domain.TelegramAlert,
	time.Duration,
) (domain.TelegramAlert, error) {
	panic("unexpected call")
}

func (f *recoveryStoreFake) FindTelegramAlertByID(
	context.Context,
	string,
) (*domain.TelegramAlert, error) {
	panic("unexpected call")
}

func (f *recoveryStoreFake) ListTelegramAlerts(
	_ context.Context,
	filter ports.TelegramAlertListFilter,
) (ports.Page[domain.TelegramAlert], error) {
	f.filter = filter
	if f.listErr != nil {
		return ports.Page[domain.TelegramAlert]{}, f.listErr
	}
	return f.page, nil
}

func (f *recoveryStoreFake) ResetActiveTelegramAlertsForDedupeKey(
	context.Context,
	string,
	string,
) (int, error) {
	panic("unexpected call")
}

func (f *recoveryStoreFake) CompareAndSwapTelegramAlert(
	_ context.Context,
	expected domain.TelegramAlert,
	next domain.TelegramAlert,
) (domain.TelegramAlert, error) {
	f.casCalls = append(f.casCalls, recoveryCASTransition{
		expected: expected,
		next:     next,
	})
	if err := f.casErrByID[expected.ID]; err != nil {
		return domain.TelegramAlert{}, err
	}
	return next, nil
}

type recoveryDelivererFake struct {
	errByID map[string]error
	calls   []string
}

func (f *recoveryDelivererFake) Deliver(
	_ context.Context,
	alertID string,
) (DeliveryResult, error) {
	f.calls = append(f.calls, alertID)
	if err := f.errByID[alertID]; err != nil {
		return DeliveryResult{}, err
	}
	return DeliveryResult{
		Alert: domain.TelegramAlert{
			ID:     alertID,
			Status: domain.TelegramAlertStatusSent,
		},
		Sent: true,
	}, nil
}

func TestRecoverFailedRetriesBoundedPersistedAlerts(t *testing.T) {
	first := failedRecoveryAlert("a")
	second := failedRecoveryAlert("b")
	store := &recoveryStoreFake{
		page: ports.Page[domain.TelegramAlert]{
			Items: []domain.TelegramAlert{first, second},
			Total: 2,
		},
		casErrByID: map[string]error{},
	}
	deliverer := &recoveryDelivererFake{
		errByID: map[string]error{},
	}
	service := mustRecoveryService(t, store, deliverer)

	result, err := service.RecoverFailed(
		context.Background(),
		2,
	)
	if err != nil {
		t.Fatalf("RecoverFailed: %v", err)
	}
	if result.Selected != 2 ||
		result.Retried != 2 ||
		result.Sent != 2 ||
		len(result.Items) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if store.filter.Status != domain.TelegramAlertStatusFailed ||
		store.filter.Page.Limit != 2 ||
		store.filter.Page.Offset != 0 {
		t.Fatalf("filter = %#v", store.filter)
	}
	if len(store.casCalls) != 2 {
		t.Fatalf("CAS calls = %d, want 2", len(store.casCalls))
	}
	for _, transition := range store.casCalls {
		if transition.expected.Status != domain.TelegramAlertStatusFailed ||
			transition.expected.Error == "" ||
			transition.next.Status != domain.TelegramAlertStatusPending ||
			transition.next.Error != "" ||
			transition.next.SentAt != nil {
			t.Fatalf("transition = %#v", transition)
		}
	}
	if len(deliverer.calls) != 2 ||
		deliverer.calls[0] != "a" ||
		deliverer.calls[1] != "b" {
		t.Fatalf("deliver calls = %#v", deliverer.calls)
	}
}

func TestRecoverFailedContinuesAfterPerItemFailures(t *testing.T) {
	store := &recoveryStoreFake{
		page: ports.Page[domain.TelegramAlert]{
			Items: []domain.TelegramAlert{
				failedRecoveryAlert("conflict"),
				failedRecoveryAlert("delivery"),
				failedRecoveryAlert("uncertain"),
				failedRecoveryAlert("sent"),
			},
			Total: 4,
		},
		casErrByID: map[string]error{
			"conflict": ports.ErrStoreConflict,
		},
	}
	deliverer := &recoveryDelivererFake{
		errByID: map[string]error{
			"delivery":  ErrDeliveryFailed,
			"uncertain": ErrDeliveryStateUncertain,
		},
	}
	service := mustRecoveryService(t, store, deliverer)

	result, err := service.RecoverFailed(
		context.Background(),
		4,
	)
	if err != nil {
		t.Fatalf("RecoverFailed: %v", err)
	}
	if result.Selected != 4 ||
		result.Retried != 3 ||
		result.Sent != 1 {
		t.Fatalf("result = %#v", result)
	}

	want := []RecoveryItem{
		{AlertID: "conflict", Status: RecoveryItemStatusCASConflict},
		{AlertID: "delivery", Status: RecoveryItemStatusDeliveryFailed},
		{AlertID: "uncertain", Status: RecoveryItemStatusStateUncertain},
		{AlertID: "sent", Status: RecoveryItemStatusSent},
	}
	if len(result.Items) != len(want) {
		t.Fatalf("items = %#v", result.Items)
	}
	for index := range want {
		if result.Items[index] != want[index] {
			t.Fatalf(
				"item[%d] = %#v, want %#v",
				index,
				result.Items[index],
				want[index],
			)
		}
	}
}

func TestRecoverFailedRejectsInvalidPersistedCandidate(t *testing.T) {
	invalid := failedRecoveryAlert("invalid")
	invalid.Error = ""
	store := &recoveryStoreFake{
		page: ports.Page[domain.TelegramAlert]{
			Items: []domain.TelegramAlert{invalid},
			Total: 1,
		},
		casErrByID: map[string]error{},
	}
	deliverer := &recoveryDelivererFake{
		errByID: map[string]error{},
	}
	service := mustRecoveryService(t, store, deliverer)

	result, err := service.RecoverFailed(
		context.Background(),
		1,
	)
	if err != nil {
		t.Fatalf("RecoverFailed: %v", err)
	}
	if result.Retried != 0 ||
		len(store.casCalls) != 0 ||
		len(deliverer.calls) != 0 ||
		len(result.Items) != 1 ||
		result.Items[0].Status != RecoveryItemStatusStateUncertain {
		t.Fatalf("result = %#v", result)
	}
}

func TestRecoverFailedRejectsOversizedPage(t *testing.T) {
	store := &recoveryStoreFake{
		page: ports.Page[domain.TelegramAlert]{
			Items: []domain.TelegramAlert{
				failedRecoveryAlert("a"),
				failedRecoveryAlert("b"),
			},
			Total: 2,
		},
		casErrByID: map[string]error{},
	}
	service := mustRecoveryService(
		t,
		store,
		&recoveryDelivererFake{errByID: map[string]error{}},
	)

	_, err := service.RecoverFailed(
		context.Background(),
		1,
	)
	if !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("error = %v, want store unavailable", err)
	}
}

func TestRecoverFailedValidatesDependenciesInputAndListFailure(t *testing.T) {
	store := &recoveryStoreFake{
		casErrByID: map[string]error{},
	}
	deliverer := &recoveryDelivererFake{
		errByID: map[string]error{},
	}

	if _, err := NewRecoveryService(nil, deliverer); !errors.Is(
		err,
		ErrDependencyRequired,
	) {
		t.Fatalf("nil store error = %v", err)
	}
	if _, err := NewRecoveryService(store, nil); !errors.Is(
		err,
		ErrDependencyRequired,
	) {
		t.Fatalf("nil deliverer error = %v", err)
	}

	service := mustRecoveryService(t, store, deliverer)
	if _, err := service.RecoverFailed(nil, 1); !errors.Is(
		err,
		ErrInvalidInput,
	) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := service.RecoverFailed(
		context.Background(),
		0,
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero limit error = %v", err)
	}

	store.listErr = ports.ErrStoreUnavailable
	if _, err := service.RecoverFailed(
		context.Background(),
		1,
	); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("list error = %v", err)
	}
}

func failedRecoveryAlert(id string) domain.TelegramAlert {
	return domain.TelegramAlert{
		ID:         id,
		AlertType:  AlertTypeResellerBalanceLow,
		DedupeKey:  "reseller-" + id,
		ResellerID: "reseller-" + id,
		Message:    "low balance",
		Status:     domain.TelegramAlertStatusFailed,
		Error:      DeliveryErrorUnavailable,
		CreatedAt: time.Date(
			2026,
			6,
			15,
			9,
			0,
			0,
			0,
			time.UTC,
		),
	}
}

func mustRecoveryService(
	t *testing.T,
	store ports.TelegramAlertStore,
	deliverer AlertDeliverer,
) *RecoveryService {
	t.Helper()
	service, err := NewRecoveryService(store, deliverer)
	if err != nil {
		t.Fatalf("NewRecoveryService: %v", err)
	}
	return service
}

var _ ports.TelegramAlertStore = (*recoveryStoreFake)(nil)
var _ AlertDeliverer = (*recoveryDelivererFake)(nil)
