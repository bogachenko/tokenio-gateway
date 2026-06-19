package telegramalert

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type staleAttemptStoreFake struct {
	started            []domain.TelegramDeliveryAttempt
	loadErr            error
	completeErrByID    map[string]error
	completeResultByID map[string]domain.TelegramDeliveryAttempt
	completeCalls      []domain.TelegramDeliveryAttempt
	cutoff             time.Time
	limit              int
}

func (f *staleAttemptStoreFake) StartTelegramDeliveryAttempt(
	context.Context,
	domain.TelegramDeliveryAttempt,
) (domain.TelegramDeliveryAttempt, error) {
	panic("unexpected call")
}

func (f *staleAttemptStoreFake) CompleteTelegramDeliveryAttempt(
	_ context.Context,
	attempt domain.TelegramDeliveryAttempt,
) (domain.TelegramDeliveryAttempt, error) {
	f.completeCalls = append(f.completeCalls, attempt)
	if err := f.completeErrByID[attempt.ID]; err != nil {
		return domain.TelegramDeliveryAttempt{}, err
	}
	if result, ok := f.completeResultByID[attempt.ID]; ok {
		return result, nil
	}
	return attempt, nil
}

func (f *staleAttemptStoreFake) LoadTelegramDeliveryAttempts(
	context.Context,
	string,
	int,
) ([]domain.TelegramDeliveryAttempt, error) {
	panic("unexpected call")
}

func (
	f *staleAttemptStoreFake,
) LoadStartedTelegramDeliveryAttemptsBefore(
	_ context.Context,
	cutoff time.Time,
	limit int,
) ([]domain.TelegramDeliveryAttempt, error) {
	f.cutoff = cutoff
	f.limit = limit
	if f.loadErr != nil {
		return nil, f.loadErr
	}
	return append(
		[]domain.TelegramDeliveryAttempt(nil),
		f.started...,
	), nil
}

type staleAttemptClock struct {
	now time.Time
}

func (c staleAttemptClock) Now() time.Time {
	return c.now
}

func TestStaleAttemptRecoveryCompletesWithoutSending(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	first := staleStartedAttempt("attempt-a", "alert-a", 1, now.Add(-10*time.Minute))
	second := staleStartedAttempt("attempt-b", "alert-b", 1, now.Add(-9*time.Minute))
	store := &staleAttemptStoreFake{
		started:            []domain.TelegramDeliveryAttempt{first, second},
		completeErrByID:    map[string]error{},
		completeResultByID: map[string]domain.TelegramDeliveryAttempt{},
	}
	service := mustStaleAttemptRecovery(
		t,
		store,
		staleAttemptClock{now: now},
		5*time.Minute,
	)

	result, err := service.Recover(context.Background(), 2)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if result.Loaded != 2 ||
		result.Completed != 2 ||
		result.Conflicts != 0 ||
		result.Uncertain != 0 ||
		len(result.Items) != 2 {
		t.Fatalf("result = %#v", result)
	}
	if !store.cutoff.Equal(now.Add(-5*time.Minute)) ||
		store.limit != 2 {
		t.Fatalf("cutoff=%s limit=%d", store.cutoff, store.limit)
	}
	if len(store.completeCalls) != 2 {
		t.Fatalf("complete calls = %d", len(store.completeCalls))
	}
	for _, completed := range store.completeCalls {
		if completed.Status !=
			domain.TelegramDeliveryAttemptStatusFailed ||
			completed.AttemptState !=
				domain.TelegramDeliveryAttemptStateSentNoResponse ||
			completed.FailureCode !=
				StaleDeliveryAttemptFailureProcessInterrupted ||
			completed.CompletedAt == nil ||
			!completed.CompletedAt.Equal(now) {
			t.Fatalf("completed = %#v", completed)
		}
	}
}

func TestStaleAttemptRecoveryAfterRestartCompletesOnlyStaleStartedAttempts(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	stale := staleStartedAttempt(
		"attempt-after-restart",
		"alert-after-restart",
		3,
		now.Add(-15*time.Minute),
	)
	fresh := staleStartedAttempt(
		"attempt-fresh",
		"alert-fresh",
		1,
		now.Add(-30*time.Second),
	)

	store := &staleAttemptStoreFake{
		started:            []domain.TelegramDeliveryAttempt{stale},
		completeErrByID:    map[string]error{},
		completeResultByID: map[string]domain.TelegramDeliveryAttempt{},
	}
	service := mustStaleAttemptRecovery(
		t,
		store,
		staleAttemptClock{now: now},
		5*time.Minute,
	)

	result, err := service.Recover(context.Background(), 100)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if result.Loaded != 1 ||
		result.Completed != 1 ||
		result.Conflicts != 0 ||
		result.Uncertain != 0 ||
		len(result.Items) != 1 {
		t.Fatalf("result = %#v", result)
	}
	if !store.cutoff.Equal(now.Add(-5 * time.Minute)) {
		t.Fatalf("cutoff = %s", store.cutoff)
	}
	if len(store.completeCalls) != 1 {
		t.Fatalf("complete calls = %#v", store.completeCalls)
	}

	completed := store.completeCalls[0]
	if completed.ID != stale.ID ||
		completed.AlertID != stale.AlertID ||
		completed.AttemptNumber != stale.AttemptNumber ||
		completed.Status != domain.TelegramDeliveryAttemptStatusFailed ||
		completed.AttemptState !=
			domain.TelegramDeliveryAttemptStateSentNoResponse ||
		completed.FailureCode !=
			StaleDeliveryAttemptFailureProcessInterrupted ||
		completed.TelegramMessageID != "" ||
		completed.CompletedAt == nil ||
		!completed.CompletedAt.Equal(now) {
		t.Fatalf("completed = %#v", completed)
	}

	if validateStaleTelegramDeliveryAttempt(fresh, store.cutoff) == nil {
		t.Fatalf("fresh started attempt was stale: %#v", fresh)
	}
}

func TestStaleAttemptRecoveryContinuesAfterPerItemFailures(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	conflict := staleStartedAttempt(
		"attempt-conflict",
		"alert-conflict",
		1,
		now.Add(-10*time.Minute),
	)
	storeFailure := staleStartedAttempt(
		"attempt-store",
		"alert-store",
		1,
		now.Add(-9*time.Minute),
	)
	success := staleStartedAttempt(
		"attempt-success",
		"alert-success",
		1,
		now.Add(-8*time.Minute),
	)
	store := &staleAttemptStoreFake{
		started: []domain.TelegramDeliveryAttempt{
			conflict,
			storeFailure,
			success,
		},
		completeErrByID: map[string]error{
			conflict.ID:     ports.ErrStoreConflict,
			storeFailure.ID: ports.ErrStoreUnavailable,
		},
		completeResultByID: map[string]domain.TelegramDeliveryAttempt{},
	}
	service := mustStaleAttemptRecovery(
		t,
		store,
		staleAttemptClock{now: now},
		5*time.Minute,
	)

	result, err := service.Recover(context.Background(), 3)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if result.Loaded != 3 ||
		result.Completed != 1 ||
		result.Conflicts != 1 ||
		result.Uncertain != 1 {
		t.Fatalf("result = %#v", result)
	}
	want := []StaleAttemptRecoveryItemStatus{
		StaleAttemptRecoveryItemConflict,
		StaleAttemptRecoveryItemUncertain,
		StaleAttemptRecoveryItemCompleted,
	}
	for index, status := range want {
		if result.Items[index].Status != status {
			t.Fatalf(
				"item[%d] status=%q want=%q",
				index,
				result.Items[index].Status,
				status,
			)
		}
	}
}

func TestStaleAttemptRecoveryRejectsInvalidLoadedCandidate(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	invalid := staleStartedAttempt(
		"attempt-invalid",
		"alert-invalid",
		1,
		now.Add(-10*time.Minute),
	)
	invalid.AttemptState =
		domain.TelegramDeliveryAttemptStateNotSent
	store := &staleAttemptStoreFake{
		started:            []domain.TelegramDeliveryAttempt{invalid},
		completeErrByID:    map[string]error{},
		completeResultByID: map[string]domain.TelegramDeliveryAttempt{},
	}
	service := mustStaleAttemptRecovery(
		t,
		store,
		staleAttemptClock{now: now},
		5*time.Minute,
	)

	result, err := service.Recover(context.Background(), 1)
	if err != nil {
		t.Fatalf("Recover: %v", err)
	}
	if result.Uncertain != 1 ||
		result.Completed != 0 ||
		len(store.completeCalls) != 0 {
		t.Fatalf("result = %#v", result)
	}
}

func TestStaleAttemptRecoveryRejectsOversizedPage(t *testing.T) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	store := &staleAttemptStoreFake{
		started: []domain.TelegramDeliveryAttempt{
			staleStartedAttempt("a", "a", 1, now.Add(-10*time.Minute)),
			staleStartedAttempt("b", "b", 1, now.Add(-9*time.Minute)),
		},
		completeErrByID:    map[string]error{},
		completeResultByID: map[string]domain.TelegramDeliveryAttempt{},
	}
	service := mustStaleAttemptRecovery(
		t,
		store,
		staleAttemptClock{now: now},
		5*time.Minute,
	)

	_, err := service.Recover(context.Background(), 1)
	if !errors.Is(err, ErrDeliveryStateUncertain) {
		t.Fatalf("error = %v", err)
	}
	if len(store.completeCalls) != 0 {
		t.Fatalf("complete calls = %d", len(store.completeCalls))
	}
}

func TestStaleAttemptRecoveryValidatesDependenciesInputClockAndLoad(
	t *testing.T,
) {
	now := time.Date(2026, 6, 15, 12, 0, 0, 0, time.UTC)
	store := &staleAttemptStoreFake{
		completeErrByID:    map[string]error{},
		completeResultByID: map[string]domain.TelegramDeliveryAttempt{},
	}

	if _, err := NewStaleAttemptRecoveryService(
		nil,
		staleAttemptClock{now: now},
		time.Minute,
	); !errors.Is(err, ErrDependencyRequired) {
		t.Fatalf("nil store error = %v", err)
	}
	if _, err := NewStaleAttemptRecoveryService(
		store,
		nil,
		time.Minute,
	); !errors.Is(err, ErrDependencyRequired) {
		t.Fatalf("nil clock error = %v", err)
	}
	if _, err := NewStaleAttemptRecoveryService(
		store,
		staleAttemptClock{now: now},
		0,
	); !errors.Is(err, ErrDependencyRequired) {
		t.Fatalf("zero staleAfter error = %v", err)
	}

	service := mustStaleAttemptRecovery(
		t,
		store,
		staleAttemptClock{now: now},
		time.Minute,
	)
	if _, err := service.Recover(nil, 1); !errors.Is(
		err,
		ErrInvalidInput,
	) {
		t.Fatalf("nil context error = %v", err)
	}
	if _, err := service.Recover(
		context.Background(),
		0,
	); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("zero batch error = %v", err)
	}

	invalidClock := mustStaleAttemptRecovery(
		t,
		store,
		staleAttemptClock{
			now: time.Date(
				2026,
				6,
				15,
				12,
				0,
				0,
				0,
				time.FixedZone("local", 3600),
			),
		},
		time.Minute,
	)
	if _, err := invalidClock.Recover(
		context.Background(),
		1,
	); !errors.Is(err, ErrDeliveryStateUncertain) {
		t.Fatalf("clock error = %v", err)
	}

	store.loadErr = ports.ErrStoreUnavailable
	if _, err := service.Recover(
		context.Background(),
		1,
	); !errors.Is(err, ErrStoreUnavailable) {
		t.Fatalf("load error = %v", err)
	}
}

func staleStartedAttempt(
	id string,
	alertID string,
	number int,
	startedAt time.Time,
) domain.TelegramDeliveryAttempt {
	return domain.TelegramDeliveryAttempt{
		ID:            id,
		AlertID:       alertID,
		AttemptNumber: number,
		Status:        domain.TelegramDeliveryAttemptStatusStarted,
		StartedAt:     startedAt,
	}
}

func mustStaleAttemptRecovery(
	t *testing.T,
	store ports.TelegramDeliveryAttemptStore,
	clock ports.Clock,
	staleAfter time.Duration,
) *StaleAttemptRecoveryService {
	t.Helper()
	service, err := NewStaleAttemptRecoveryService(
		store,
		clock,
		staleAfter,
	)
	if err != nil {
		t.Fatalf("NewStaleAttemptRecoveryService: %v", err)
	}
	return service
}

var _ ports.TelegramDeliveryAttemptStore = (*staleAttemptStoreFake)(nil)
