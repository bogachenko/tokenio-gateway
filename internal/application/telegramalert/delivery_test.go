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

func (f *deliveryStoreFake) ResetActiveTelegramAlertsForDedupeKey(
	context.Context,
	string,
	string,
) (int, error) {
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
	return next, nil
}

type deliveryAttemptStoreFake struct {
	history       []domain.TelegramDeliveryAttempt
	loadErr       error
	startErr      error
	completeErr   error
	startCalls    []domain.TelegramDeliveryAttempt
	completeCalls []domain.TelegramDeliveryAttempt
}

func (f *deliveryAttemptStoreFake) StartTelegramDeliveryAttempt(
	_ context.Context,
	attempt domain.TelegramDeliveryAttempt,
) (domain.TelegramDeliveryAttempt, error) {
	f.startCalls = append(f.startCalls, attempt)
	if f.startErr != nil {
		return domain.TelegramDeliveryAttempt{}, f.startErr
	}
	return attempt, nil
}

func (f *deliveryAttemptStoreFake) CompleteTelegramDeliveryAttempt(
	_ context.Context,
	attempt domain.TelegramDeliveryAttempt,
) (domain.TelegramDeliveryAttempt, error) {
	f.completeCalls = append(f.completeCalls, attempt)
	if f.completeErr != nil {
		return domain.TelegramDeliveryAttempt{}, f.completeErr
	}
	return attempt, nil
}

func (f *deliveryAttemptStoreFake) LoadTelegramDeliveryAttempts(
	context.Context,
	string,
	int,
) ([]domain.TelegramDeliveryAttempt, error) {
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return append([]domain.TelegramDeliveryAttempt(nil), f.history...), nil
}

func (f *deliveryAttemptStoreFake) LoadStartedTelegramDeliveryAttemptsBefore(
	context.Context,
	time.Time,
	int,
) ([]domain.TelegramDeliveryAttempt, error) {
	panic("unexpected call")
}

type deliverySenderFake struct {
	outcome MessageDeliveryOutcome
	err     error
	calls   int
	message string
}

func (f *deliverySenderFake) SendMessage(
	_ context.Context,
	message string,
) (MessageDeliveryOutcome, error) {
	f.calls++
	f.message = message
	return f.outcome, f.err
}

type sequenceClock struct {
	values []time.Time
	index  int
}

func (c *sequenceClock) Now() time.Time {
	if c.index >= len(c.values) {
		return time.Time{}
	}
	value := c.values[c.index]
	c.index++
	return value
}

func TestDeliveryPersistsAttemptBeforeSendAndTransitionsToSent(t *testing.T) {
	startedAt := time.Date(2026, 6, 15, 10, 0, 0, 0, time.UTC)
	completedAt := startedAt.Add(time.Second)
	alert := pendingAlert()
	alerts := &deliveryStoreFake{current: &alert}
	attempts := &deliveryAttemptStoreFake{}
	sender := &deliverySenderFake{
		outcome: MessageDeliveryOutcomeResponseReceived,
	}
	service := mustDeliveryService(
		t,
		alerts,
		attempts,
		sender,
		&sequenceClock{values: []time.Time{startedAt, completedAt}},
	)

	result, err := service.Deliver(context.Background(), alert.ID)
	if err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if !result.Sent ||
		result.Alert.Status != domain.TelegramAlertStatusSent {
		t.Fatalf("result = %#v", result)
	}
	if len(attempts.startCalls) != 1 ||
		len(attempts.completeCalls) != 1 ||
		sender.calls != 1 ||
		alerts.casCalls != 1 {
		t.Fatalf(
			"start=%d complete=%d send=%d CAS=%d",
			len(attempts.startCalls),
			len(attempts.completeCalls),
			sender.calls,
			alerts.casCalls,
		)
	}
	started := attempts.startCalls[0]
	completed := attempts.completeCalls[0]
	if started.Status != domain.TelegramDeliveryAttemptStatusStarted ||
		started.AttemptNumber != 1 ||
		started.AlertID != alert.ID ||
		started.StartedAt != startedAt {
		t.Fatalf("started = %#v", started)
	}
	if completed.Status !=
		domain.TelegramDeliveryAttemptStatusSucceeded ||
		completed.AttemptState !=
			domain.TelegramDeliveryAttemptStateResponseReceived ||
		completed.CompletedAt == nil ||
		!completed.CompletedAt.Equal(completedAt) {
		t.Fatalf("completed = %#v", completed)
	}
}

func TestDeliverySafeFailurePersistsFailedAttemptAndAlert(t *testing.T) {
	startedAt := pendingAlert().CreatedAt.Add(time.Minute)
	completedAt := startedAt.Add(time.Second)
	alert := pendingAlert()
	alerts := &deliveryStoreFake{current: &alert}
	attempts := &deliveryAttemptStoreFake{}
	sender := &deliverySenderFake{
		outcome: MessageDeliveryOutcomeNotSent,
		err:     errors.New("secret raw transport error"),
	}
	service := mustDeliveryService(
		t,
		alerts,
		attempts,
		sender,
		&sequenceClock{values: []time.Time{startedAt, completedAt}},
	)

	result, err := service.Deliver(context.Background(), alert.ID)
	if !errors.Is(err, ErrDeliveryFailed) {
		t.Fatalf("error = %v", err)
	}
	if result.Alert.Status != domain.TelegramAlertStatusFailed ||
		result.Alert.Error != DeliveryErrorUnavailable {
		t.Fatalf("result = %#v", result)
	}
	terminal := attempts.completeCalls[0]
	if terminal.Status != domain.TelegramDeliveryAttemptStatusFailed ||
		terminal.AttemptState !=
			domain.TelegramDeliveryAttemptStateNotSent ||
		terminal.FailureCode != deliveryAttemptFailureNotSent {
		t.Fatalf("terminal = %#v", terminal)
	}
	if terminal.FailureCode == sender.err.Error() {
		t.Fatal("raw sender error persisted")
	}
}

func TestDeliverySentNoResponseLeavesAlertPending(t *testing.T) {
	startedAt := pendingAlert().CreatedAt.Add(time.Minute)
	completedAt := startedAt.Add(time.Second)
	alert := pendingAlert()
	alerts := &deliveryStoreFake{current: &alert}
	attempts := &deliveryAttemptStoreFake{}
	sender := &deliverySenderFake{
		outcome: MessageDeliveryOutcomeSentNoResponse,
		err:     errors.New("timeout"),
	}
	service := mustDeliveryService(
		t,
		alerts,
		attempts,
		sender,
		&sequenceClock{values: []time.Time{startedAt, completedAt}},
	)

	_, err := service.Deliver(context.Background(), alert.ID)
	if !errors.Is(err, ErrDeliveryStateUncertain) {
		t.Fatalf("error = %v", err)
	}
	if alerts.casCalls != 0 {
		t.Fatalf("alert CAS calls = %d", alerts.casCalls)
	}
	terminal := attempts.completeCalls[0]
	if terminal.AttemptState !=
		domain.TelegramDeliveryAttemptStateSentNoResponse ||
		terminal.FailureCode !=
			deliveryAttemptFailureSentNoResponse {
		t.Fatalf("terminal = %#v", terminal)
	}
}

func TestDeliveryDoesNotSendWhenHistoryIsUncertain(t *testing.T) {
	alert := pendingAlert()
	started := domain.TelegramDeliveryAttempt{
		ID:            deliveryAttemptID(alert.ID, 1),
		AlertID:       alert.ID,
		AttemptNumber: 1,
		Status:        domain.TelegramDeliveryAttemptStatusStarted,
		StartedAt:     alert.CreatedAt.Add(time.Minute),
	}
	for name, history := range map[string][]domain.TelegramDeliveryAttempt{
		"started": {started},
		"sent no response": {{
			ID:            started.ID,
			AlertID:       started.AlertID,
			AttemptNumber: 1,
			Status:        domain.TelegramDeliveryAttemptStatusFailed,
			AttemptState:  domain.TelegramDeliveryAttemptStateSentNoResponse,
			FailureCode:   deliveryAttemptFailureSentNoResponse,
			StartedAt:     started.StartedAt,
			CompletedAt:   timePointer(started.StartedAt.Add(time.Second)),
		}},
	} {
		t.Run(name, func(t *testing.T) {
			alerts := &deliveryStoreFake{current: &alert}
			attempts := &deliveryAttemptStoreFake{history: history}
			sender := &deliverySenderFake{
				outcome: MessageDeliveryOutcomeResponseReceived,
			}
			service := mustDeliveryService(
				t,
				alerts,
				attempts,
				sender,
				&sequenceClock{values: []time.Time{
					alert.CreatedAt.Add(2 * time.Minute),
				}},
			)

			_, err := service.Deliver(context.Background(), alert.ID)
			if !errors.Is(err, ErrDeliveryStateUncertain) {
				t.Fatalf("error = %v", err)
			}
			if sender.calls != 0 ||
				len(attempts.startCalls) != 0 ||
				alerts.casCalls != 0 {
				t.Fatalf(
					"send=%d start=%d CAS=%d",
					sender.calls,
					len(attempts.startCalls),
					alerts.casCalls,
				)
			}
		})
	}
}

func TestDeliveryStartsNextAttemptAfterSafeFailure(t *testing.T) {
	alert := pendingAlert()
	completedAt := alert.CreatedAt.Add(time.Minute)
	history := []domain.TelegramDeliveryAttempt{{
		ID:            deliveryAttemptID(alert.ID, 1),
		AlertID:       alert.ID,
		AttemptNumber: 1,
		Status:        domain.TelegramDeliveryAttemptStatusFailed,
		AttemptState:  domain.TelegramDeliveryAttemptStateNotSent,
		FailureCode:   deliveryAttemptFailureNotSent,
		StartedAt:     alert.CreatedAt.Add(30 * time.Second),
		CompletedAt:   &completedAt,
	}}
	alerts := &deliveryStoreFake{current: &alert}
	attempts := &deliveryAttemptStoreFake{history: history}
	sender := &deliverySenderFake{
		outcome: MessageDeliveryOutcomeResponseReceived,
	}
	startedAt := completedAt.Add(time.Second)
	service := mustDeliveryService(
		t,
		alerts,
		attempts,
		sender,
		&sequenceClock{values: []time.Time{
			startedAt,
			startedAt.Add(time.Second),
		}},
	)

	if _, err := service.Deliver(
		context.Background(),
		alert.ID,
	); err != nil {
		t.Fatalf("Deliver: %v", err)
	}
	if len(attempts.startCalls) != 1 ||
		attempts.startCalls[0].AttemptNumber != 2 {
		t.Fatalf("start calls = %#v", attempts.startCalls)
	}
}

func TestDeliveryDoesNotSendWhenAttemptPersistenceFails(t *testing.T) {
	alert := pendingAlert()
	alerts := &deliveryStoreFake{current: &alert}
	attempts := &deliveryAttemptStoreFake{
		startErr: ports.ErrStoreUnavailable,
	}
	sender := &deliverySenderFake{
		outcome: MessageDeliveryOutcomeResponseReceived,
	}
	service := mustDeliveryService(
		t,
		alerts,
		attempts,
		sender,
		&sequenceClock{values: []time.Time{
			alert.CreatedAt.Add(time.Minute),
		}},
	)

	_, err := service.Deliver(context.Background(), alert.ID)
	if !errors.Is(err, ErrDeliveryStateUncertain) {
		t.Fatalf("error = %v", err)
	}
	if sender.calls != 0 || alerts.casCalls != 0 {
		t.Fatalf("send=%d CAS=%d", sender.calls, alerts.casCalls)
	}
}

func TestDeliveryDoesNotMutateAlertWhenAttemptCompletionFails(t *testing.T) {
	alert := pendingAlert()
	alerts := &deliveryStoreFake{current: &alert}
	attempts := &deliveryAttemptStoreFake{
		completeErr: ports.ErrStoreUnavailable,
	}
	sender := &deliverySenderFake{
		outcome: MessageDeliveryOutcomeResponseReceived,
	}
	startedAt := alert.CreatedAt.Add(time.Minute)
	service := mustDeliveryService(
		t,
		alerts,
		attempts,
		sender,
		&sequenceClock{values: []time.Time{
			startedAt,
			startedAt.Add(time.Second),
		}},
	)

	_, err := service.Deliver(context.Background(), alert.ID)
	if !errors.Is(err, ErrDeliveryStateUncertain) {
		t.Fatalf("error = %v", err)
	}
	if alerts.casCalls != 0 {
		t.Fatalf("CAS calls = %d", alerts.casCalls)
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
				alert.SentAt = timePointer(
					alert.CreatedAt.Add(time.Minute),
				)
			}
			if status == domain.TelegramAlertStatusFailed {
				alert.Error = DeliveryErrorUnavailable
			}
			alerts := &deliveryStoreFake{current: &alert}
			attempts := &deliveryAttemptStoreFake{}
			sender := &deliverySenderFake{}
			service := mustDeliveryService(
				t,
				alerts,
				attempts,
				sender,
				&sequenceClock{},
			)

			_, err := service.Deliver(context.Background(), alert.ID)
			if !errors.Is(err, ErrAlertNotPending) {
				t.Fatalf("error = %v", err)
			}
			if sender.calls != 0 ||
				len(attempts.startCalls) != 0 ||
				alerts.casCalls != 0 {
				t.Fatal("non-pending alert produced side effects")
			}
		})
	}
}

func TestDeliveryValidatesDependenciesInputAndLookup(t *testing.T) {
	alert := pendingAlert()
	alerts := &deliveryStoreFake{current: &alert}
	attempts := &deliveryAttemptStoreFake{}
	sender := &deliverySenderFake{}
	clock := &sequenceClock{}

	builders := []func() error{
		func() error {
			_, err := NewDeliveryService(nil, attempts, sender, clock)
			return err
		},
		func() error {
			_, err := NewDeliveryService(alerts, nil, sender, clock)
			return err
		},
		func() error {
			_, err := NewDeliveryService(alerts, attempts, nil, clock)
			return err
		},
		func() error {
			_, err := NewDeliveryService(alerts, attempts, sender, nil)
			return err
		},
	}
	for _, build := range builders {
		if err := build(); !errors.Is(err, ErrDependencyRequired) {
			t.Fatalf("dependency error = %v", err)
		}
	}

	service := mustDeliveryService(
		t,
		alerts,
		attempts,
		sender,
		clock,
	)
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
	service = mustDeliveryService(
		t,
		missing,
		attempts,
		sender,
		clock,
	)
	if _, err := service.Deliver(
		context.Background(),
		"missing",
	); !errors.Is(err, ErrAlertNotFound) {
		t.Fatalf("missing error = %v", err)
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

func timePointer(value time.Time) *time.Time {
	return &value
}

func mustDeliveryService(
	t *testing.T,
	alerts ports.TelegramAlertStore,
	attempts ports.TelegramDeliveryAttemptStore,
	sender MessageSender,
	clock ports.Clock,
) *DeliveryService {
	t.Helper()
	service, err := NewDeliveryService(
		alerts,
		attempts,
		sender,
		clock,
	)
	if err != nil {
		t.Fatalf("NewDeliveryService: %v", err)
	}
	return service
}

func (f *deliveryStoreFake) CompareAndSwapTelegramAlertWithAudit(
	ctx context.Context,
	expected domain.TelegramAlert,
	next domain.TelegramAlert,
	_ domain.AuditContext,
) (domain.TelegramAlert, error) {
	// ponytail: test fake delegates audit-free transition; production atomic audit is covered in postgres/admin tests.
	return f.CompareAndSwapTelegramAlert(ctx, expected, next)
}

var _ ports.TelegramAlertStore = (*deliveryStoreFake)(nil)
var _ ports.TelegramDeliveryAttemptStore = (*deliveryAttemptStoreFake)(nil)
var _ MessageSender = (*deliverySenderFake)(nil)
