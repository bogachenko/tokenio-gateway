package telegramalert

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const StaleDeliveryAttemptFailureProcessInterrupted = "telegram_process_interrupted"

type StaleAttemptRecoveryItemStatus string

const (
	StaleAttemptRecoveryItemCompleted StaleAttemptRecoveryItemStatus = "completed"
	StaleAttemptRecoveryItemConflict  StaleAttemptRecoveryItemStatus = "conflict"
	StaleAttemptRecoveryItemUncertain StaleAttemptRecoveryItemStatus = "uncertain"
)

type StaleAttemptRecoveryItem struct {
	AttemptID     string
	AlertID       string
	AttemptNumber int
	Status        StaleAttemptRecoveryItemStatus
}

type StaleAttemptRecoveryResult struct {
	Loaded    int
	Completed int
	Conflicts int
	Uncertain int
	Items     []StaleAttemptRecoveryItem
}

type StaleAttemptRecoveryService struct {
	attempts   ports.TelegramDeliveryAttemptStore
	clock      ports.Clock
	staleAfter time.Duration
}

func NewStaleAttemptRecoveryService(
	attempts ports.TelegramDeliveryAttemptStore,
	clock ports.Clock,
	staleAfter time.Duration,
) (*StaleAttemptRecoveryService, error) {
	if attempts == nil || clock == nil || staleAfter <= 0 {
		return nil, ErrDependencyRequired
	}
	return &StaleAttemptRecoveryService{
		attempts:   attempts,
		clock:      clock,
		staleAfter: staleAfter,
	}, nil
}

func (s *StaleAttemptRecoveryService) Recover(
	ctx context.Context,
	batchSize int,
) (StaleAttemptRecoveryResult, error) {
	if s == nil ||
		s.attempts == nil ||
		s.clock == nil ||
		s.staleAfter <= 0 {
		return StaleAttemptRecoveryResult{}, ErrDependencyRequired
	}
	if ctx == nil || batchSize <= 0 {
		return StaleAttemptRecoveryResult{}, fmt.Errorf(
			"%w: invalid stale Telegram attempt recovery input",
			ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return StaleAttemptRecoveryResult{}, err
	}

	now := s.clock.Now()
	if now.IsZero() || now.Location() != time.UTC {
		return StaleAttemptRecoveryResult{}, fmt.Errorf(
			"%w: invalid stale Telegram attempt recovery clock",
			ErrDeliveryStateUncertain,
		)
	}
	cutoff := now.Add(-s.staleAfter)

	started, err :=
		s.attempts.LoadStartedTelegramDeliveryAttemptsBefore(
			ctx,
			cutoff,
			batchSize,
		)
	if err != nil {
		return StaleAttemptRecoveryResult{}, fmt.Errorf(
			"%w: load stale Telegram delivery attempts",
			ErrStoreUnavailable,
		)
	}
	if len(started) > batchSize {
		return StaleAttemptRecoveryResult{}, fmt.Errorf(
			"%w: recovery store exceeded batch size",
			ErrDeliveryStateUncertain,
		)
	}

	result := StaleAttemptRecoveryResult{
		Loaded: len(started),
		Items:  make([]StaleAttemptRecoveryItem, 0, len(started)),
	}

	for _, attempt := range started {
		item := StaleAttemptRecoveryItem{
			AttemptID:     attempt.ID,
			AlertID:       attempt.AlertID,
			AttemptNumber: attempt.AttemptNumber,
		}

		if err := validateStaleTelegramDeliveryAttempt(
			attempt,
			cutoff,
		); err != nil {
			item.Status = StaleAttemptRecoveryItemUncertain
			result.Uncertain++
			result.Items = append(result.Items, item)
			continue
		}

		terminal := interruptedTelegramDeliveryAttempt(
			attempt,
			now,
		)
		persisted, err :=
			s.attempts.CompleteTelegramDeliveryAttempt(
				context.WithoutCancel(ctx),
				terminal,
			)
		if err != nil {
			if errors.Is(err, ports.ErrStoreConflict) {
				item.Status = StaleAttemptRecoveryItemConflict
				result.Conflicts++
			} else {
				item.Status = StaleAttemptRecoveryItemUncertain
				result.Uncertain++
			}
			result.Items = append(result.Items, item)
			continue
		}
		if !sameTerminalDeliveryAttempt(persisted, terminal) {
			item.Status = StaleAttemptRecoveryItemUncertain
			result.Uncertain++
			result.Items = append(result.Items, item)
			continue
		}

		item.Status = StaleAttemptRecoveryItemCompleted
		result.Completed++
		result.Items = append(result.Items, item)
	}

	return result, nil
}

func validateStaleTelegramDeliveryAttempt(
	attempt domain.TelegramDeliveryAttempt,
	cutoff time.Time,
) error {
	if attempt.ID == "" ||
		attempt.AlertID == "" ||
		attempt.AttemptNumber <= 0 ||
		attempt.Status !=
			domain.TelegramDeliveryAttemptStatusStarted ||
		attempt.AttemptState != "" ||
		attempt.FailureCode != "" ||
		attempt.CompletedAt != nil ||
		attempt.StartedAt.IsZero() ||
		attempt.StartedAt.Location() != time.UTC ||
		!attempt.StartedAt.Before(cutoff) {
		return ErrDeliveryStateUncertain
	}
	return nil
}

func interruptedTelegramDeliveryAttempt(
	started domain.TelegramDeliveryAttempt,
	completedAt time.Time,
) domain.TelegramDeliveryAttempt {
	result := started
	result.Status = domain.TelegramDeliveryAttemptStatusFailed
	result.AttemptState =
		domain.TelegramDeliveryAttemptStateSentNoResponse
	result.FailureCode =
		StaleDeliveryAttemptFailureProcessInterrupted
	result.CompletedAt = &completedAt
	return result
}
