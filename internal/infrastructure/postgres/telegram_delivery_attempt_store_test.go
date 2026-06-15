package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestValidateStartedTelegramDeliveryAttempt(t *testing.T) {
	attempt := validStartedTelegramDeliveryAttempt()
	if err := validateStartedTelegramDeliveryAttempt(attempt); err != nil {
		t.Fatalf("validate started: %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*domain.TelegramDeliveryAttempt)
	}{
		{
			name: "blank ID",
			mutate: func(value *domain.TelegramDeliveryAttempt) {
				value.ID = ""
			},
		},
		{
			name: "blank alert",
			mutate: func(value *domain.TelegramDeliveryAttempt) {
				value.AlertID = ""
			},
		},
		{
			name: "non-positive number",
			mutate: func(value *domain.TelegramDeliveryAttempt) {
				value.AttemptNumber = 0
			},
		},
		{
			name: "terminal state",
			mutate: func(value *domain.TelegramDeliveryAttempt) {
				value.AttemptState =
					domain.TelegramDeliveryAttemptStateNotSent
			},
		},
		{
			name: "failure code",
			mutate: func(value *domain.TelegramDeliveryAttempt) {
				value.FailureCode = "unavailable"
			},
		},
		{
			name: "completed",
			mutate: func(value *domain.TelegramDeliveryAttempt) {
				completedAt := value.StartedAt
				value.CompletedAt = &completedAt
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			value := attempt
			test.mutate(&value)
			if !errors.Is(
				validateStartedTelegramDeliveryAttempt(value),
				ports.ErrStoreContractViolation,
			) {
				t.Fatalf("attempt accepted: %+v", value)
			}
		})
	}
}

func TestValidateTerminalTelegramDeliveryAttempt(t *testing.T) {
	started := validStartedTelegramDeliveryAttempt()
	completedAt := started.StartedAt.Add(time.Second)

	succeeded := started
	succeeded.Status =
		domain.TelegramDeliveryAttemptStatusSucceeded
	succeeded.AttemptState =
		domain.TelegramDeliveryAttemptStateResponseReceived
	succeeded.CompletedAt = &completedAt
	if err := validateTerminalTelegramDeliveryAttempt(succeeded); err != nil {
		t.Fatalf("validate succeeded: %v", err)
	}

	failed := started
	failed.Status = domain.TelegramDeliveryAttemptStatusFailed
	failed.AttemptState =
		domain.TelegramDeliveryAttemptStateNotSent
	failed.FailureCode = "telegram_unavailable"
	failed.CompletedAt = &completedAt
	if err := validateTerminalTelegramDeliveryAttempt(failed); err != nil {
		t.Fatalf("validate failed: %v", err)
	}

	failed.AttemptState = ""
	if !errors.Is(
		validateTerminalTelegramDeliveryAttempt(failed),
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf("failed attempt without state accepted: %+v", failed)
	}
}

func TestTelegramDeliveryRecoveryOrder(t *testing.T) {
	base := validStartedTelegramDeliveryAttempt()

	later := base
	later.StartedAt = base.StartedAt.Add(time.Second)
	if !telegramDeliveryRecoveryOrderBefore(base, later) {
		t.Fatal("earlier started_at must sort first")
	}

	sameTimeNextAlert := base
	sameTimeNextAlert.AlertID = "tgalt_b"
	if !telegramDeliveryRecoveryOrderBefore(base, sameTimeNextAlert) {
		t.Fatal("alert_id tie-breaker is invalid")
	}

	sameAlertNextAttempt := base
	sameAlertNextAttempt.AttemptNumber = 2
	if !telegramDeliveryRecoveryOrderBefore(
		base,
		sameAlertNextAttempt,
	) {
		t.Fatal("attempt_number tie-breaker is invalid")
	}
	if telegramDeliveryRecoveryOrderBefore(base, base) {
		t.Fatal("equal attempts must not compare before")
	}
}

func TestTelegramDeliveryAttemptIdentityAndTerminalEquality(t *testing.T) {
	started := validStartedTelegramDeliveryAttempt()
	if !telegramDeliveryAttemptsEqual(started, started) {
		t.Fatal("identical started attempts differ")
	}

	completedAt := started.StartedAt.Add(time.Second)
	left := started
	left.Status = domain.TelegramDeliveryAttemptStatusFailed
	left.AttemptState =
		domain.TelegramDeliveryAttemptStateNotSent
	left.FailureCode = "telegram_unavailable"
	left.CompletedAt = &completedAt

	right := left
	copiedCompletedAt := completedAt
	right.CompletedAt = &copiedCompletedAt
	if !telegramDeliveryAttemptsEqual(left, right) {
		t.Fatal("identical terminal attempts differ")
	}

	right.FailureCode = "different"
	if telegramDeliveryAttemptsEqual(left, right) {
		t.Fatal("conflicting terminal attempts compare equal")
	}
}

func TestCanonicalTelegramDeliveryAttemptUsesPostgresPrecision(
	t *testing.T,
) {
	startedAt := time.Date(
		2026,
		time.June,
		15,
		12,
		0,
		0,
		123456789,
		time.UTC,
	)
	completedAt := startedAt.Add(time.Second)
	value := validStartedTelegramDeliveryAttempt()
	value.StartedAt = startedAt
	value.CompletedAt = &completedAt

	canonical := canonicalTelegramDeliveryAttempt(value)
	if canonical.StartedAt.Nanosecond() != 123456000 {
		t.Fatalf("started_at = %s", canonical.StartedAt)
	}
	if canonical.CompletedAt == nil ||
		canonical.CompletedAt.Nanosecond() != 123456000 {
		t.Fatalf("completed_at = %v", canonical.CompletedAt)
	}
}

func validStartedTelegramDeliveryAttempt() domain.TelegramDeliveryAttempt {
	return domain.TelegramDeliveryAttempt{
		ID:            "tgattempt_1",
		AlertID:       "tgalt_a",
		AttemptNumber: 1,
		Status:        domain.TelegramDeliveryAttemptStatusStarted,
		StartedAt: time.Date(
			2026,
			time.June,
			15,
			12,
			0,
			0,
			0,
			time.UTC,
		),
	}
}
