package llmrequest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type ForwardingExecutionInput struct {
	Prepared    PreparedRequest
	Admission   BillingAdmissionResult
	Reservation ReservationResult
}

type ForwardingExecutionResult struct {
	Response ports.ForwardResponse
}

type ForwardingExecutor interface {
	Forward(
		context.Context,
		ForwardingExecutionInput,
	) (ForwardingExecutionResult, error)
}

type ForwardingFailure interface {
	error
	FailureKindValue() string
	FailureStatusCode() int
	FailureAttemptStateValue() string
	FailureRouteRetryCandidate() bool
}

type ForwardingStage struct {
	capacity    ports.RouteCapacityManager
	reservation AtomicReservation
	attempts    ports.ForwardingAttemptStore
	clock       ports.Clock
	forwarder   ForwardingExecutor
}

type ForwardedRequest struct {
	Reserved         ReservedRequest
	Response         ports.ForwardResponse
	ResolvedUsage    UsageResolutionResult
	FinalUsageRecord domain.UsageRecord
	AutoCharge       AutoChargeResult
}

func NewForwardingStage(
	capacity ports.RouteCapacityManager,
	reservation AtomicReservation,
	attempts ports.ForwardingAttemptStore,
	clock ports.Clock,
	forwarder ForwardingExecutor,
) (*ForwardingStage, error) {
	if capacity == nil ||
		reservation == nil ||
		attempts == nil ||
		clock == nil ||
		forwarder == nil {
		return nil, ErrDependencyRequired
	}
	return &ForwardingStage{
		capacity:    capacity,
		reservation: reservation,
		attempts:    attempts,
		clock:       clock,
		forwarder:   forwarder,
	}, nil
}

func (s *ForwardingStage) Execute(
	ctx context.Context,
	prepared PreparedRequest,
	admission BillingAdmissionResult,
) (
	result ForwardedRequest,
	err error,
) {
	if s == nil ||
		s.capacity == nil ||
		s.reservation == nil ||
		s.attempts == nil ||
		s.clock == nil ||
		s.forwarder == nil {
		return ForwardedRequest{}, ErrDependencyRequired
	}
	if ctx == nil {
		return ForwardedRequest{}, fmt.Errorf(
			"%w: nil forwarding stage context",
			ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return ForwardedRequest{}, err
	}
	if err := validateBillingAdmission(
		prepared,
		admission,
	); err != nil {
		return ForwardedRequest{}, err
	}

	lease, err := s.capacity.Acquire(
		ctx,
		ports.RouteCapacityAcquireInput{
			LocalRequestID: prepared.LocalRequestID,
			Route:          prepared.Plan.Route,
			Reseller:       prepared.Plan.Reseller,
			EstimatedUsage: prepared.Plan.EstimatedUsage,
		},
	)
	if err != nil {
		return ForwardedRequest{}, fmt.Errorf(
			"acquire route capacity: %w",
			err,
		)
	}

	defer func() {
		releaseErr := s.capacity.Release(
			context.WithoutCancel(ctx),
			lease,
		)
		if releaseErr != nil {
			err = errors.Join(
				err,
				fmt.Errorf(
					"release route capacity: %w",
					releaseErr,
				),
			)
		}
	}()

	if lease.LocalRequestID != prepared.LocalRequestID ||
		lease.RouteID != prepared.Plan.Route.ID {
		return ForwardedRequest{}, fmt.Errorf(
			"%w: invalid route capacity reservation",
			ErrStageContractViolation,
		)
	}

	reservation, err := s.reservation.Reserve(
		ctx,
		reservationInput(prepared),
	)
	if err != nil {
		return ForwardedRequest{}, fmt.Errorf(
			"reserve usage and reseller balance: %w",
			err,
		)
	}
	if err := validateReservation(
		prepared,
		reservation,
	); err != nil {
		return ForwardedRequest{}, err
	}

	startedAt, err := forwardingStageNow(s.clock)
	if err != nil {
		return ForwardedRequest{}, err
	}
	startedAttempt := forwardingAttemptStarted(
		prepared,
		1,
		startedAt,
	)
	persistedStarted, err := s.attempts.StartAttempt(
		ctx,
		startedAttempt,
	)
	if err != nil {
		return ForwardedRequest{}, fmt.Errorf(
			"start forwarding attempt: %w",
			err,
		)
	}
	if !forwardingAttemptsEqual(
		persistedStarted,
		startedAttempt,
	) {
		return ForwardedRequest{}, fmt.Errorf(
			"%w: invalid started forwarding attempt",
			ErrStageContractViolation,
		)
	}

	execution, forwardErr := s.forwarder.Forward(
		ctx,
		ForwardingExecutionInput{
			Prepared:    clonePreparedRequest(prepared),
			Admission:   admission,
			Reservation: cloneReservationResult(reservation),
		},
	)
	completedAt, clockErr := forwardingStageNow(s.clock)
	if clockErr != nil {
		return ForwardedRequest{}, errors.Join(
			forwardErr,
			clockErr,
		)
	}
	if forwardErr != nil {
		terminal, classificationErr :=
			failedForwardingAttempt(
				startedAttempt,
				completedAt,
				forwardErr,
			)
		if classificationErr != nil {
			return ForwardedRequest{}, errors.Join(
				forwardErr,
				classificationErr,
			)
		}
		persisted, completionErr := s.attempts.CompleteAttempt(
			context.WithoutCancel(ctx),
			terminal,
		)
		if completionErr != nil {
			return ForwardedRequest{}, errors.Join(
				forwardErr,
				fmt.Errorf(
					"complete failed forwarding attempt: %w",
					completionErr,
				),
			)
		}
		if !forwardingAttemptsEqual(persisted, terminal) {
			return ForwardedRequest{}, errors.Join(
				forwardErr,
				fmt.Errorf(
					"%w: invalid failed forwarding attempt",
					ErrStageContractViolation,
				),
			)
		}
		return ForwardedRequest{}, fmt.Errorf(
			"forward selected route: %w",
			forwardErr,
		)
	}
	terminal, err := succeededForwardingAttempt(
		startedAttempt,
		completedAt,
		execution.Response.StatusCode,
	)
	if err != nil {
		return ForwardedRequest{}, err
	}
	persistedTerminal, err := s.attempts.CompleteAttempt(
		context.WithoutCancel(ctx),
		terminal,
	)
	if err != nil {
		return ForwardedRequest{}, fmt.Errorf(
			"complete successful forwarding attempt: %w",
			err,
		)
	}
	if !forwardingAttemptsEqual(
		persistedTerminal,
		terminal,
	) {
		return ForwardedRequest{}, fmt.Errorf(
			"%w: invalid successful forwarding attempt",
			ErrStageContractViolation,
		)
	}

	return ForwardedRequest{
		Reserved: ReservedRequest{
			Prepared:    clonePreparedRequest(prepared),
			Admission:   admission,
			Reservation: cloneReservationResult(reservation),
		},
		Response: cloneForwardResponse(execution.Response),
	}, nil
}

func forwardingStageNow(clock ports.Clock) (time.Time, error) {
	now := clock.Now()
	if now.IsZero() || now.Location() != time.UTC {
		return time.Time{}, fmt.Errorf(
			"%w: invalid forwarding stage clock",
			ErrStageContractViolation,
		)
	}
	return now, nil
}

func forwardingAttemptStarted(
	prepared PreparedRequest,
	attemptNumber int,
	startedAt time.Time,
) domain.ForwardingAttempt {
	route := prepared.Plan.Route
	return domain.ForwardingAttempt{
		LocalRequestID: prepared.LocalRequestID,
		AttemptNumber:  attemptNumber,
		RouteID:        route.ID,
		ResellerID:     prepared.Plan.Reseller.ID,
		APIFamily:      prepared.APIFamily,
		EndpointKind:   prepared.EndpointKind,
		ClientModel:    prepared.ClientModel,
		ProviderType:   route.ProviderType,
		ProviderModel:  route.ProviderModel,
		Status: domain.
			ForwardingAttemptStatusStarted,
		StartedAt: startedAt,
	}
}

func succeededForwardingAttempt(
	started domain.ForwardingAttempt,
	completedAt time.Time,
	statusCode int,
) (domain.ForwardingAttempt, error) {
	if statusCode < 200 || statusCode > 299 ||
		completedAt.Before(started.StartedAt) {
		return domain.ForwardingAttempt{}, fmt.Errorf(
			"%w: invalid successful forwarding result",
			ErrStageContractViolation,
		)
	}
	result := started
	result.Status = domain.ForwardingAttemptStatusSucceeded
	result.AttemptState =
		domain.ForwardingAttemptStateResponseReceived
	result.UpstreamStatusCode = statusCode
	result.CompletedAt = &completedAt
	return result, nil
}

func failedForwardingAttempt(
	started domain.ForwardingAttempt,
	completedAt time.Time,
	forwardErr error,
) (domain.ForwardingAttempt, error) {
	var failure ForwardingFailure
	if !errors.As(forwardErr, &failure) || failure == nil {
		return domain.ForwardingAttempt{}, fmt.Errorf(
			"%w: forwarding executor returned unclassified error",
			ErrStageContractViolation,
		)
	}
	attemptState := domain.ForwardingAttemptState(
		failure.FailureAttemptStateValue(),
	)
	switch attemptState {
	case domain.ForwardingAttemptStateNotSent,
		domain.ForwardingAttemptStateSentNoResponse,
		domain.ForwardingAttemptStateResponseReceived:
	default:
		return domain.ForwardingAttempt{}, fmt.Errorf(
			"%w: invalid forwarding failure attempt state",
			ErrStageContractViolation,
		)
	}
	statusCode := failure.FailureStatusCode()
	if statusCode < 0 || statusCode > 599 ||
		(statusCode > 0 && statusCode < 100) ||
		failure.FailureKindValue() == "" ||
		completedAt.Before(started.StartedAt) {
		return domain.ForwardingAttempt{}, fmt.Errorf(
			"%w: invalid forwarding failure classification",
			ErrStageContractViolation,
		)
	}
	result := started
	result.Status = domain.ForwardingAttemptStatusFailed
	result.AttemptState = attemptState
	result.UpstreamStatusCode = statusCode
	result.FailureKind = failure.FailureKindValue()
	result.RouteRetryCandidate =
		failure.FailureRouteRetryCandidate()
	result.CompletedAt = &completedAt
	return result, nil
}

func forwardingAttemptsEqual(
	left domain.ForwardingAttempt,
	right domain.ForwardingAttempt,
) bool {
	return left.LocalRequestID == right.LocalRequestID &&
		left.AttemptNumber == right.AttemptNumber &&
		left.RouteID == right.RouteID &&
		left.ResellerID == right.ResellerID &&
		left.APIFamily == right.APIFamily &&
		left.EndpointKind == right.EndpointKind &&
		left.ClientModel == right.ClientModel &&
		left.ProviderType == right.ProviderType &&
		left.ProviderModel == right.ProviderModel &&
		left.Status == right.Status &&
		left.AttemptState == right.AttemptState &&
		left.UpstreamStatusCode == right.UpstreamStatusCode &&
		left.FailureKind == right.FailureKind &&
		left.RouteRetryCandidate == right.RouteRetryCandidate &&
		left.StartedAt.Equal(right.StartedAt) &&
		equalForwardingAttemptTimes(
			left.CompletedAt,
			right.CompletedAt,
		)
}

func equalForwardingAttemptTimes(
	left *time.Time,
	right *time.Time,
) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.Equal(*right)
	}
}

func cloneForwardResponse(
	value ports.ForwardResponse,
) ports.ForwardResponse {
	return ports.ForwardResponse{
		StatusCode: value.StatusCode,
		Headers:    cloneForwardHeaders(value.Headers),
		Body:       cloneBytes(value.Body),
	}
}

func cloneForwardHeaders(
	value map[string][]string,
) map[string][]string {
	if value == nil {
		return nil
	}
	result := make(map[string][]string, len(value))
	for key, values := range value {
		result[key] = append([]string(nil), values...)
	}
	return result
}
