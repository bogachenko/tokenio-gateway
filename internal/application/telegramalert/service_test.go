package telegramalert

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type resellerReaderFake struct {
	value *domain.Reseller
	err   error
	calls int
}

func (f *resellerReaderFake) FindResellerByID(
	context.Context,
	string,
) (*domain.Reseller, error) {
	f.calls++
	return f.value, f.err
}

type alertStoreFake struct {
	result       domain.TelegramAlert
	resetCalls   int
	resetType    string
	resetKey     string
	resetResult  int
	err          error
	calls        int
	requested    domain.TelegramAlert
	dedupePeriod time.Duration
}

func (f *alertStoreFake) CreateOrSuppressTelegramAlert(
	_ context.Context,
	requested domain.TelegramAlert,
	dedupePeriod time.Duration,
) (domain.TelegramAlert, error) {
	f.calls++
	f.requested = requested
	f.dedupePeriod = dedupePeriod
	if f.err != nil {
		return domain.TelegramAlert{}, f.err
	}
	if f.result.ID == "" {
		return requested, nil
	}
	return f.result, nil
}

func (f *alertStoreFake) ResetActiveTelegramAlertsForDedupeKey(
	_ context.Context,
	alertType string,
	dedupeKey string,
) (int, error) {
	f.resetCalls++
	f.resetType = alertType
	f.resetKey = dedupeKey
	return f.resetResult, nil
}

func (f *alertStoreFake) FindTelegramAlertByID(
	context.Context,
	string,
) (*domain.TelegramAlert, error) {
	panic("unexpected call")
}

func (f *alertStoreFake) ListTelegramAlerts(
	context.Context,
	ports.TelegramAlertListFilter,
) (ports.Page[domain.TelegramAlert], error) {
	panic("unexpected call")
}

func (f *alertStoreFake) CompareAndSwapTelegramAlert(
	context.Context,
	domain.TelegramAlert,
	domain.TelegramAlert,
) (domain.TelegramAlert, error) {
	panic("unexpected call")
}

type clockFake struct {
	now time.Time
}

func (f clockFake) Now() time.Time {
	return f.now
}

func TestCheckResellerDoesNotPersistAboveThreshold(t *testing.T) {
	reader := &resellerReaderFake{
		value: &domain.Reseller{
			ID:            "reseller-1",
			ProviderType:  domain.ProviderOpenAI,
			BalanceCents:  20_000,
			ReservedCents: 4_999,
		},
	}
	store := &alertStoreFake{}
	service := mustService(t, reader, store)

	result, err := service.CheckReseller(
		context.Background(),
		"reseller-1",
	)
	if err != nil {
		t.Fatalf("CheckReseller: %v", err)
	}
	if result.BelowThreshold {
		t.Fatal("BelowThreshold = true, want false")
	}
	if result.AvailableBalanceCents != 15_001 ||
		result.ThresholdCents != 15_000 ||
		result.Alert != nil {
		t.Fatalf("result = %#v", result)
	}
	if store.calls != 0 {
		t.Fatalf("store calls = %d, want 0", store.calls)
	}
	if store.resetCalls != 1 ||
		store.resetType != AlertTypeResellerBalanceLow ||
		store.resetKey != "reseller-1" {
		t.Fatalf(
			"reset calls=%d type=%q key=%q",
			store.resetCalls,
			store.resetType,
			store.resetKey,
		)
	}
}

func TestCheckResellerPersistsPendingAlertAtThreshold(t *testing.T) {
	at := time.Date(2026, 6, 15, 12, 30, 0, 0, time.FixedZone("offset", 3*60*60))
	reader := &resellerReaderFake{
		value: &domain.Reseller{
			ID:            "reseller-1",
			ProviderType:  domain.ProviderAnthropic,
			BalanceCents:  20_000,
			ReservedCents: 5_000,
			APIKeyEnv:     "SECRET_ENV_MUST_NOT_LEAK",
		},
	}
	store := &alertStoreFake{}
	service := mustServiceWithClock(
		t,
		reader,
		store,
		clockFake{now: at},
	)

	result, err := service.CheckReseller(
		context.Background(),
		"reseller-1",
	)
	if err != nil {
		t.Fatalf("CheckReseller: %v", err)
	}
	if !result.BelowThreshold ||
		result.AvailableBalanceCents != 15_000 ||
		result.Alert == nil {
		t.Fatalf("result = %#v", result)
	}
	if store.calls != 1 {
		t.Fatalf("store calls = %d, want 1", store.calls)
	}
	if store.dedupePeriod != time.Hour {
		t.Fatalf(
			"dedupe period = %s, want 1h",
			store.dedupePeriod,
		)
	}
	alert := store.requested
	if alert.AlertType != AlertTypeResellerBalanceLow ||
		alert.DedupeKey != "reseller-1" ||
		alert.ResellerID != "reseller-1" ||
		alert.RouteID != "" ||
		alert.Status != domain.TelegramAlertStatusPending ||
		alert.CreatedAt != at.UTC() {
		t.Fatalf("alert = %#v", alert)
	}
	for _, expected := range []string{
		"reseller_id: reseller-1",
		"provider_type: anthropic",
		"available_balance_cents: 15000",
		"threshold_cents: 15000",
		"time: 2026-06-15T09:30:00Z",
	} {
		if !strings.Contains(alert.Message, expected) {
			t.Fatalf(
				"message %q does not contain %q",
				alert.Message,
				expected,
			)
		}
	}
	if strings.Contains(alert.Message, "SECRET_ENV_MUST_NOT_LEAK") {
		t.Fatalf("message leaks API key environment: %q", alert.Message)
	}
}

func TestCheckResellerPreservesSuppressedPersistenceResult(t *testing.T) {
	at := time.Date(2026, 6, 15, 9, 30, 0, 0, time.UTC)
	reader := &resellerReaderFake{
		value: &domain.Reseller{
			ID:            "reseller-1",
			ProviderType:  domain.ProviderOpenAI,
			BalanceCents:  10,
			ReservedCents: 1,
		},
	}
	store := &alertStoreFake{}
	service := mustServiceWithClock(
		t,
		reader,
		store,
		clockFake{now: at},
	)
	requestedID := stableAlertID("reseller-1", 9, at)
	store.result = domain.TelegramAlert{
		ID:         requestedID,
		AlertType:  AlertTypeResellerBalanceLow,
		DedupeKey:  "reseller-1",
		ResellerID: "reseller-1",
		Message: formatLowBalanceMessage(
			*reader.value,
			9,
			15_000,
			at,
		),
		Status:    domain.TelegramAlertStatusSuppressed,
		CreatedAt: at,
	}

	result, err := service.CheckReseller(
		context.Background(),
		"reseller-1",
	)
	if err != nil {
		t.Fatalf("CheckReseller: %v", err)
	}
	if result.Alert == nil ||
		result.Alert.Status != domain.TelegramAlertStatusSuppressed {
		t.Fatalf("result = %#v", result)
	}
}

func TestCheckResellerRejectsInvalidContracts(t *testing.T) {
	now := time.Date(2026, 6, 15, 9, 30, 0, 0, time.UTC)
	tests := []struct {
		name    string
		reader  *resellerReaderFake
		store   *alertStoreFake
		clock   ports.Clock
		id      string
		wantErr error
	}{
		{
			name:    "blank reseller id",
			reader:  &resellerReaderFake{},
			store:   &alertStoreFake{},
			clock:   clockFake{now: now},
			id:      " ",
			wantErr: ErrInvalidInput,
		},
		{
			name:    "missing reseller",
			reader:  &resellerReaderFake{err: ports.ErrNotFound},
			store:   &alertStoreFake{},
			clock:   clockFake{now: now},
			id:      "missing",
			wantErr: ErrResellerNotFound,
		},
		{
			name: "negative reserved balance",
			reader: &resellerReaderFake{value: &domain.Reseller{
				ID:            "reseller-1",
				ReservedCents: -1,
			}},
			store:   &alertStoreFake{},
			clock:   clockFake{now: now},
			id:      "reseller-1",
			wantErr: ErrInvalidBalance,
		},
		{
			name: "balance subtraction overflow",
			reader: &resellerReaderFake{value: &domain.Reseller{
				ID:            "reseller-1",
				BalanceCents:  math.MinInt64,
				ReservedCents: 1,
			}},
			store:   &alertStoreFake{},
			clock:   clockFake{now: now},
			id:      "reseller-1",
			wantErr: ErrInvalidBalance,
		},
		{
			name: "zero clock",
			reader: &resellerReaderFake{value: &domain.Reseller{
				ID:           "reseller-1",
				BalanceCents: 1,
			}},
			store:   &alertStoreFake{},
			clock:   clockFake{},
			id:      "reseller-1",
			wantErr: ErrClockUnavailable,
		},
		{
			name: "store failure",
			reader: &resellerReaderFake{value: &domain.Reseller{
				ID:           "reseller-1",
				BalanceCents: 1,
			}},
			store:   &alertStoreFake{err: ports.ErrStoreUnavailable},
			clock:   clockFake{now: now},
			id:      "reseller-1",
			wantErr: ErrStoreUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			service, err := NewService(
				test.reader,
				test.store,
				test.clock,
				Config{
					ThresholdCents: 15_000,
					DedupePeriod:   time.Hour,
				},
			)
			if err != nil {
				t.Fatalf("NewService: %v", err)
			}
			_, err = service.CheckReseller(
				context.Background(),
				test.id,
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf(
					"error = %v, want %v",
					err,
					test.wantErr,
				)
			}
		})
	}
}

func TestNewServiceValidatesDependenciesAndConfig(t *testing.T) {
	reader := &resellerReaderFake{}
	store := &alertStoreFake{}
	clock := clockFake{
		now: time.Date(2026, 6, 15, 9, 30, 0, 0, time.UTC),
	}

	tests := []struct {
		name    string
		reader  ResellerReader
		store   ports.TelegramAlertStore
		clock   ports.Clock
		config  Config
		wantErr error
	}{
		{
			name:    "nil reader",
			store:   store,
			clock:   clock,
			config:  Config{DedupePeriod: time.Hour},
			wantErr: ErrDependencyRequired,
		},
		{
			name:    "nil store",
			reader:  reader,
			clock:   clock,
			config:  Config{DedupePeriod: time.Hour},
			wantErr: ErrDependencyRequired,
		},
		{
			name:    "nil clock",
			reader:  reader,
			store:   store,
			config:  Config{DedupePeriod: time.Hour},
			wantErr: ErrDependencyRequired,
		},
		{
			name:   "negative threshold",
			reader: reader,
			store:  store,
			clock:  clock,
			config: Config{
				ThresholdCents: -1,
				DedupePeriod:   time.Hour,
			},
			wantErr: ErrInvalidInput,
		},
		{
			name:    "nonpositive dedupe period",
			reader:  reader,
			store:   store,
			clock:   clock,
			config:  Config{},
			wantErr: ErrInvalidInput,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := NewService(
				test.reader,
				test.store,
				test.clock,
				test.config,
			)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf(
					"error = %v, want %v",
					err,
					test.wantErr,
				)
			}
		})
	}
}

func mustService(
	t *testing.T,
	reader ResellerReader,
	store ports.TelegramAlertStore,
) *Service {
	t.Helper()
	return mustServiceWithClock(
		t,
		reader,
		store,
		clockFake{
			now: time.Date(2026, 6, 15, 9, 30, 0, 0, time.UTC),
		},
	)
}

func mustServiceWithClock(
	t *testing.T,
	reader ResellerReader,
	store ports.TelegramAlertStore,
	clock ports.Clock,
) *Service {
	t.Helper()
	service, err := NewService(
		reader,
		store,
		clock,
		Config{
			ThresholdCents: 15_000,
			DedupePeriod:   time.Hour,
		},
	)
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return service
}

var _ ResellerReader = (*resellerReaderFake)(nil)

func (f *alertStoreFake) CompareAndSwapTelegramAlertWithAudit(
	ctx context.Context,
	expected domain.TelegramAlert,
	next domain.TelegramAlert,
	_ domain.AuditContext,
) (domain.TelegramAlert, error) {
	// ponytail: test fake delegates audit-free transition; production atomic audit is covered in postgres/admin tests.
	return f.CompareAndSwapTelegramAlert(ctx, expected, next)
}

var _ ports.TelegramAlertStore = (*alertStoreFake)(nil)
