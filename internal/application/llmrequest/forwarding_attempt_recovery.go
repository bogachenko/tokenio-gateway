package llmrequest

import (
	"context"
	"fmt"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const ForwardingFailureKindProcessInterrupted = "process_interrupted"

type ForwardingAttemptRecoveryResult struct {
	Loaded    int
	Completed int
}

type ForwardingAttemptRecovery struct {
	attempts   ports.ForwardingAttemptStore
	clock      ports.Clock
	staleAfter time.Duration
}

func NewForwardingAttemptRecovery(
	attempts ports.ForwardingAttemptStore,
	clock ports.Clock,
	staleAfter time.Duration,
) (*ForwardingAttemptRecovery, error) {
	if attempts == nil ||
		clock == nil ||
		staleAfter <= 0 {
		return nil, ErrDependencyRequired
	}
	return &ForwardingAttemptRecovery{
		attempts:   attempts,
		clock:      clock,
		staleAfter: staleAfter,
	}, nil
}

func (r *ForwardingAttemptRecovery) Recover(
	ctx context.Context,
	batchSize int,
) (ForwardingAttemptRecoveryResult, error) {
	if r == nil ||
		r.attempts == nil ||
		r.clock == nil ||
		r.staleAfter <= 0 {
		return ForwardingAttemptRecoveryResult{},
			ErrDependencyRequired
	}
	if ctx == nil || batchSize <= 0 {
		return ForwardingAttemptRecoveryResult{}, fmt.Errorf(
			"%w: invalid forwarding recovery input",
			ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return ForwardingAttemptRecoveryResult{}, err
	}

	now := r.clock.Now()
	if now.IsZero() || now.Location() != time.UTC {
		return ForwardingAttemptRecoveryResult{}, fmt.Errorf(
			"%w: invalid forwarding recovery clock",
			ErrStageContractViolation,
		)
	}
	cutoff := now.Add(-r.staleAfter)
	started, err := r.attempts.LoadStartedBefore(
		ctx,
		cutoff,
		batchSize,
	)
	if err != nil {
		return ForwardingAttemptRecoveryResult{}, fmt.Errorf(
			"load stale forwarding attempts: %w",
			err,
		)
	}
	if len(started) > batchSize {
		return ForwardingAttemptRecoveryResult{}, fmt.Errorf(
			"%w: recovery store exceeded batch size",
			ErrStageContractViolation,
		)
	}

	result := ForwardingAttemptRecoveryResult{
		Loaded: len(started),
	}
	for _, attempt := range started {
		if err := validateRecoverableForwardingAttempt(
			attempt,
			cutoff,
		); err != nil {
			return result, err
		}
		terminal := interruptedForwardingAttempt(
			attempt,
			now,
		)
		persisted, err := r.attempts.CompleteAttempt(
			context.WithoutCancel(ctx),
			terminal,
		)
		if err != nil {
			return result, fmt.Errorf(
				"complete interrupted forwarding attempt %q/%d: %w",
				attempt.LocalRequestID,
				attempt.AttemptNumber,
				err,
			)
		}
		if !forwardingAttemptsEqual(persisted, terminal) {
			return result, fmt.Errorf(
				"%w: invalid recovered forwarding attempt",
				ErrStageContractViolation,
			)
		}
		result.Completed++
	}
	return result, nil
}

func validateRecoverableForwardingAttempt(
	attempt domain.ForwardingAttempt,
	cutoff time.Time,
) error {
	if attempt.Status != domain.ForwardingAttemptStatusStarted ||
		attempt.AttemptState != "" ||
		attempt.UpstreamStatusCode != 0 ||
		attempt.FailureKind != "" ||
		attempt.RouteRetryCandidate ||
		attempt.CompletedAt != nil ||
		attempt.StartedAt.IsZero() ||
		attempt.StartedAt.Location() != time.UTC ||
		!attempt.StartedAt.Before(cutoff) {
		return fmt.Errorf(
			"%w: invalid stale forwarding attempt",
			ErrStageContractViolation,
		)
	}
	return nil
}

func interruptedForwardingAttempt(
	started domain.ForwardingAttempt,
	completedAt time.Time,
) domain.ForwardingAttempt {
	result := started
	result.Status = domain.ForwardingAttemptStatusFailed
	result.AttemptState =
		domain.ForwardingAttemptStateSentNoResponse
	result.FailureKind =
		ForwardingFailureKindProcessInterrupted
	result.RouteRetryCandidate = false
	result.CompletedAt = &completedAt
	return result
}
