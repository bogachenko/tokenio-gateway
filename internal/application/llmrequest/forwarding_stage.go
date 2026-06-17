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

type ForwardingRetryAfter interface {
	FailureRetryAfterPresent() bool
	FailureRetryAfterDelay() time.Duration
	FailureRetryAfterTime() time.Time
}

type RetryWaiter interface {
	Wait(context.Context, time.Duration) error
}

type ForwardingStage struct {
	capacity    ports.RouteCapacityManager
	reservation AtomicReservation
	transfer    RouteReservationTransfer
	attempts    ports.ForwardingAttemptStore
	cooldowns   ports.RouteCooldownStore
	clock       ports.Clock
	forwarder   ForwardingExecutor
	policy      RoutingPolicy
	waiter      RetryWaiter
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
	transfer RouteReservationTransfer,
	attempts ports.ForwardingAttemptStore,
	cooldowns ports.RouteCooldownStore,
	clock ports.Clock,
	forwarder ForwardingExecutor,
	policy RoutingPolicy,
	waiter RetryWaiter,
) (*ForwardingStage, error) {
	if capacity == nil ||
		reservation == nil ||
		transfer == nil ||
		attempts == nil ||
		cooldowns == nil ||
		clock == nil ||
		forwarder == nil ||
		waiter == nil {
		return nil, ErrDependencyRequired
	}
	if policy.UpstreamTimeout() <= 0 ||
		policy.UpstreamMaxAttempts() < 1 {
		return nil, ErrInvalidInput
	}
	return &ForwardingStage{
		capacity:    capacity,
		reservation: reservation,
		transfer:    transfer,
		attempts:    attempts,
		cooldowns:   cooldowns,
		clock:       clock,
		forwarder:   forwarder,
		policy:      policy,
		waiter:      waiter,
	}, nil
}

func (s *ForwardingStage) Execute(
	ctx context.Context,
	prepared PreparedRequest,
	admission BillingAdmissionResult,
) (result ForwardedRequest, err error) {
	if s == nil || s.capacity == nil || s.reservation == nil ||
		s.transfer == nil || s.attempts == nil || s.cooldowns == nil ||
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
	if err := validateBillingAdmission(prepared, admission); err != nil {
		return ForwardedRequest{}, err
	}

	candidates := forwardingCandidates(prepared.Plan)
	if len(candidates) > s.policy.UpstreamMaxAttempts() {
		candidates = candidates[:s.policy.UpstreamMaxAttempts()]
	}
	var reservation ReservationResult
	reservationCreated := false
	var lastForwardErr error
	var lastCapacityErr error

	for index, candidate := range candidates {
		attemptNumber := index + 1
		candidatePrepared := preparedForForwardingCandidate(
			prepared,
			candidate,
			candidates[index+1:],
		)
		lease, acquireErr := s.capacity.Acquire(
			ctx,
			ports.RouteCapacityAcquireInput{
				LocalRequestID: candidatePrepared.LocalRequestID,
				ReservationID: forwardingCapacityReservationID(
					candidatePrepared.LocalRequestID,
					attemptNumber,
				),
				Route:          candidatePrepared.Plan.Route,
				Reseller:       candidatePrepared.Plan.Reseller,
				EstimatedUsage: candidatePrepared.Plan.EstimatedUsage,
			},
		)
		if acquireErr != nil {
			if errors.Is(acquireErr, ports.ErrRouteCapacityUnavailable) {
				lastCapacityErr = fmt.Errorf(
					"route %q capacity unavailable: %w",
					candidatePrepared.Plan.Route.ID,
					acquireErr,
				)
				eventAt, eventErr := forwardingStageNow(s.clock)
				if eventErr != nil {
					return ForwardedRequest{}, errors.Join(lastCapacityErr, eventErr)
				}
				if eventErr := s.appendForwardingEvent(
					ctx,
					candidatePrepared,
					attemptNumber,
					domain.RouteEventTypeCapacityRejected,
					routeEventReasonCapacity,
					eventAt,
					domain.RouteEventMetadata{"attempt_number": attemptNumber},
				); eventErr != nil {
					return ForwardedRequest{}, errors.Join(lastCapacityErr, eventErr)
				}
				continue
			}
			return ForwardedRequest{}, fmt.Errorf(
				"acquire route capacity: %w",
				acquireErr,
			)
		}
		if err := validateForwardingCapacityLease(
			lease,
			candidatePrepared,
			attemptNumber,
		); err != nil {
			releaseErr := s.capacity.Release(
				context.WithoutCancel(ctx),
				lease,
			)
			return ForwardedRequest{}, errors.Join(err, releaseErr)
		}

		attemptResult, attemptErr := s.executeLeasedCandidate(
			ctx,
			candidatePrepared,
			admission,
			reservation,
			reservationCreated,
			candidate,
			attemptNumber,
			lease,
		)
		if attemptResult.Reservation.Usage.LocalRequestID != "" {
			reservation = attemptResult.Reservation
			reservationCreated = true
		}
		if attemptErr == nil {
			return ForwardedRequest{
				Reserved: ReservedRequest{
					Prepared:  clonePreparedRequest(attemptResult.Prepared),
					Admission: admission,
					Reservation: cloneReservationResult(
						attemptResult.Reservation,
					),
				},
				Response: cloneForwardResponse(
					attemptResult.Execution.Response,
				),
			}, nil
		}
		lastForwardErr = attemptErr
		if !attemptResult.RetryAllowed {
			return ForwardedRequest{}, attemptErr
		}
		if err := ctx.Err(); err != nil {
			return ForwardedRequest{}, errors.Join(attemptErr, err)
		}
		if index+1 < len(candidates) {
			delay, delayErr := s.retryDelay(
				attemptResult,
				attemptNumber,
			)
			if delayErr != nil {
				return ForwardedRequest{}, errors.Join(
					attemptErr,
					delayErr,
				)
			}
			eventAt, eventErr := forwardingStageNow(s.clock)
			if eventErr != nil {
				return ForwardedRequest{}, errors.Join(attemptErr, eventErr)
			}
			if eventErr := s.appendForwardingEvent(
				ctx,
				candidatePrepared,
				attemptNumber,
				domain.RouteEventTypeRetryScheduled,
				routeEventReasonRetryScheduled,
				eventAt,
				domain.RouteEventMetadata{
					"attempt_number": attemptNumber,
					"delay_ms":       delay.Milliseconds(),
				},
			); eventErr != nil {
				return ForwardedRequest{}, errors.Join(attemptErr, eventErr)
			}
			if delay > 0 {
				if waitErr := s.waiter.Wait(ctx, delay); waitErr != nil {
					return ForwardedRequest{}, errors.Join(
						attemptErr,
						waitErr,
					)
				}
			}
		}
	}

	switch {
	case lastForwardErr != nil && lastCapacityErr != nil:
		return ForwardedRequest{}, errors.Join(lastForwardErr, lastCapacityErr)
	case lastForwardErr != nil:
		return ForwardedRequest{}, lastForwardErr
	case lastCapacityErr != nil:
		return ForwardedRequest{}, errors.Join(
			ErrRouteUnavailable,
			lastCapacityErr,
		)
	default:
		return ForwardedRequest{}, fmt.Errorf(
			"%w: empty forwarding candidate plan",
			ErrStageContractViolation,
		)
	}
}

type forwardingCandidate struct {
	Plan RouteFallbackPlan
}

type leasedCandidateResult struct {
	Prepared          PreparedRequest
	Reservation       ReservationResult
	Execution         ForwardingExecutionResult
	RetryAllowed      bool
	FailureKind       string
	RetryAfterPresent bool
	RetryAfterDelay   time.Duration
	RetryAfterAt      time.Time
}

func (s *ForwardingStage) executeLeasedCandidate(
	ctx context.Context,
	prepared PreparedRequest,
	admission BillingAdmissionResult,
	current ReservationResult,
	reservationCreated bool,
	candidate forwardingCandidate,
	attemptNumber int,
	lease ports.RouteCapacityReservation,
) (result leasedCandidateResult, err error) {
	defer func() {
		releaseErr := s.capacity.Release(
			context.WithoutCancel(ctx),
			lease,
		)
		if releaseErr != nil {
			err = errors.Join(
				err,
				fmt.Errorf("release route capacity: %w", releaseErr),
			)
			result.RetryAllowed = false
		}
	}()

	reservation := current
	if !reservationCreated {
		created, reserveErr := s.reservation.Reserve(
			ctx,
			reservationInput(prepared),
		)
		if reserveErr != nil {
			return leasedCandidateResult{}, fmt.Errorf(
				"reserve usage and reseller balance: %w",
				reserveErr,
			)
		}
		if err := validateReservation(prepared, created); err != nil {
			return leasedCandidateResult{}, err
		}
		reservation = created
	} else {
		transferred, transferErr := s.transfer.Transfer(
			ctx,
			RouteReservationTransferInput{
				ExpectedUsage: current.Usage,
				Target:        candidate.Plan,
			},
		)
		if transferErr != nil {
			return leasedCandidateResult{}, fmt.Errorf(
				"transfer route reservation: %w",
				transferErr,
			)
		}
		var validationErr error
		reservation, validationErr = validateTransferredReservation(
			prepared,
			current,
			transferred,
		)
		if validationErr != nil {
			return leasedCandidateResult{}, validationErr
		}
	}

	result.Prepared = clonePreparedRequest(prepared)
	result.Reservation = cloneReservationResult(reservation)

	startedAt, err := forwardingStageNow(s.clock)
	if err != nil {
		return result, err
	}
	started := forwardingAttemptStarted(prepared, attemptNumber, startedAt)
	persistedStarted, err := s.attempts.StartAttempt(ctx, started)
	if err != nil {
		return result, fmt.Errorf("start forwarding attempt: %w", err)
	}
	if !forwardingAttemptsEqual(persistedStarted, started) {
		return result, fmt.Errorf(
			"%w: invalid started forwarding attempt",
			ErrStageContractViolation,
		)
	}
	if err := s.appendForwardingEvent(
		ctx,
		prepared,
		attemptNumber,
		domain.RouteEventTypeForwardingStarted,
		routeEventReasonAttemptStarted,
		startedAt,
		domain.RouteEventMetadata{"attempt_number": attemptNumber},
	); err != nil {
		return result, err
	}

	attemptContext, cancelAttempt := context.WithTimeout(
		ctx,
		s.policy.UpstreamTimeout(),
	)
	execution, forwardErr := s.forwarder.Forward(
		attemptContext,
		ForwardingExecutionInput{
			Prepared:    clonePreparedRequest(prepared),
			Admission:   admission,
			Reservation: cloneReservationResult(reservation),
		},
	)
	cancelAttempt()
	result.Execution = execution
	completedAt, clockErr := forwardingStageNow(s.clock)
	if clockErr != nil {
		return result, errors.Join(forwardErr, clockErr)
	}
	if forwardErr == nil {
		terminal, terminalErr := succeededForwardingAttempt(
			started,
			completedAt,
			execution.Response.StatusCode,
		)
		if terminalErr != nil {
			return result, terminalErr
		}
		persisted, completionErr := s.attempts.CompleteAttempt(
			context.WithoutCancel(ctx),
			terminal,
		)
		if completionErr != nil {
			return result, fmt.Errorf(
				"complete successful forwarding attempt: %w",
				completionErr,
			)
		}
		if !forwardingAttemptsEqual(persisted, terminal) {
			return result, fmt.Errorf(
				"%w: invalid successful forwarding attempt",
				ErrStageContractViolation,
			)
		}
		if err := s.appendForwardingEvent(
			ctx,
			prepared,
			attemptNumber,
			domain.RouteEventTypeForwardingSucceeded,
			routeEventReasonSucceeded,
			completedAt,
			domain.RouteEventMetadata{
				"attempt_number": attemptNumber,
				"status_code":    execution.Response.StatusCode,
			},
		); err != nil {
			return result, err
		}
		return result, nil
	}

	terminal, classificationErr := failedForwardingAttempt(
		started,
		completedAt,
		forwardErr,
	)
	if classificationErr != nil {
		return result, errors.Join(forwardErr, classificationErr)
	}
	persisted, completionErr := s.attempts.CompleteAttempt(
		context.WithoutCancel(ctx),
		terminal,
	)
	if completionErr != nil {
		return result, errors.Join(
			forwardErr,
			fmt.Errorf(
				"complete failed forwarding attempt: %w",
				completionErr,
			),
		)
	}
	if !forwardingAttemptsEqual(persisted, terminal) {
		return result, errors.Join(
			forwardErr,
			fmt.Errorf(
				"%w: invalid failed forwarding attempt",
				ErrStageContractViolation,
			),
		)
	}
	if eventErr := s.appendForwardingEvent(
		ctx,
		prepared,
		attemptNumber,
		domain.RouteEventTypeForwardingFailed,
		terminal.FailureKind,
		completedAt,
		domain.RouteEventMetadata{
			"attempt_number": attemptNumber,
			"failure_kind":   terminal.FailureKind,
			"status_code":    terminal.UpstreamStatusCode,
		},
	); eventErr != nil {
		return result, errors.Join(forwardErr, eventErr)
	}
	if cooldownErr := s.persistRouteCooldown(
		ctx,
		prepared,
		terminal,
	); cooldownErr != nil {
		return result, errors.Join(forwardErr, cooldownErr)
	}
	result.RetryAllowed = forwardingAttemptAllowsRetry(ctx, terminal)
	result.FailureKind = terminal.FailureKind
	present, delay, at, metadataErr := forwardingFailureRetryAfter(
		forwardErr,
	)
	if metadataErr != nil {
		return result, errors.Join(forwardErr, metadataErr)
	}
	result.RetryAfterPresent = present
	result.RetryAfterDelay = delay
	result.RetryAfterAt = at
	return result, fmt.Errorf(
		"forward route %q attempt %d: %w",
		prepared.Plan.Route.ID,
		attemptNumber,
		forwardErr,
	)
}

func forwardingCandidates(plan RoutePlan) []forwardingCandidate {
	result := make([]forwardingCandidate, 0, 1+len(plan.Fallbacks))
	result = append(result, forwardingCandidate{Plan: RouteFallbackPlan{
		Route:                      plan.Route,
		Reseller:                   plan.Reseller,
		Price:                      plan.Price,
		BillingModel:               plan.BillingModel,
		EstimatedUsage:             plan.EstimatedUsage,
		EstimatedClientAmountCents: plan.EstimatedClientAmountCents,
		EstimatedUpstreamCostCents: plan.EstimatedUpstreamCostCents,
		Currency:                   plan.Currency,
		Confidence:                 plan.Confidence,
	}})
	for _, fallback := range plan.Fallbacks {
		result = append(result, forwardingCandidate{
			Plan: cloneRouteFallbackPlan(fallback),
		})
	}
	return result
}

func preparedForForwardingCandidate(
	base PreparedRequest,
	candidate forwardingCandidate,
	remaining []forwardingCandidate,
) PreparedRequest {
	result := clonePreparedRequest(base)
	result.Plan = RoutePlan{
		Route:                      candidate.Plan.Route,
		Reseller:                   candidate.Plan.Reseller,
		Price:                      candidate.Plan.Price,
		BillingModel:               candidate.Plan.BillingModel,
		EstimatedUsage:             candidate.Plan.EstimatedUsage,
		EstimatedClientAmountCents: candidate.Plan.EstimatedClientAmountCents,
		EstimatedUpstreamCostCents: candidate.Plan.EstimatedUpstreamCostCents,
		Currency:                   candidate.Plan.Currency,
		Confidence:                 candidate.Plan.Confidence,
		Fallbacks:                  make([]RouteFallbackPlan, 0, len(remaining)),
	}
	for _, next := range remaining {
		result.Plan.Fallbacks = append(
			result.Plan.Fallbacks,
			cloneRouteFallbackPlan(next.Plan),
		)
	}
	return result
}

func cloneRouteFallbackPlan(value RouteFallbackPlan) RouteFallbackPlan {
	return RouteFallbackPlan{
		Route:                      value.Route,
		Reseller:                   value.Reseller,
		Price:                      value.Price,
		BillingModel:               value.BillingModel,
		EstimatedUsage:             value.EstimatedUsage,
		EstimatedClientAmountCents: value.EstimatedClientAmountCents,
		EstimatedUpstreamCostCents: value.EstimatedUpstreamCostCents,
		Currency:                   value.Currency,
		Confidence:                 value.Confidence,
	}
}

func validateForwardingCapacityLease(
	lease ports.RouteCapacityReservation,
	prepared PreparedRequest,
	attemptNumber int,
) error {
	if lease.LocalRequestID != prepared.LocalRequestID ||
		lease.ReservationID != forwardingCapacityReservationID(
			prepared.LocalRequestID,
			attemptNumber,
		) ||
		lease.RouteID != prepared.Plan.Route.ID {
		return fmt.Errorf(
			"%w: invalid route capacity reservation",
			ErrStageContractViolation,
		)
	}
	return nil
}

func validateTransferredReservation(
	prepared PreparedRequest,
	previous ReservationResult,
	transferred RouteReservationTransferResult,
) (ReservationResult, error) {
	usage := transferred.Usage
	if usage.LocalRequestID != prepared.LocalRequestID ||
		usage.Status != domain.UsageStatusReserved ||
		usage.SelectedRouteID != prepared.Plan.Route.ID ||
		usage.SelectedResellerID != prepared.Plan.Reseller.ID ||
		usage.ProviderType != prepared.Plan.Route.ProviderType ||
		usage.ProviderModel != prepared.Plan.Route.ProviderModel ||
		usage.BillingModel != prepared.Plan.BillingModel ||
		usage.EstimatedUsage != prepared.Plan.EstimatedUsage ||
		usage.EstimatedClientAmountCents != prepared.Plan.EstimatedClientAmountCents ||
		usage.EstimatedUpstreamCostCents != prepared.Plan.EstimatedUpstreamCostCents ||
		usage.Currency != prepared.Plan.Currency ||
		transferred.ReservedReseller.ID != prepared.Plan.Reseller.ID {
		return ReservationResult{}, fmt.Errorf(
			"%w: invalid transferred route reservation",
			ErrStageContractViolation,
		)
	}
	reseller := transferred.ReservedReseller
	return ReservationResult{
		Disposition: previous.Disposition,
		Usage:       usage,
		Reseller:    &reseller,
	}, nil
}

func forwardingAttemptAllowsRetry(
	ctx context.Context,
	attempt domain.ForwardingAttempt,
) bool {
	if ctx == nil || ctx.Err() != nil || !attempt.RouteRetryCandidate {
		return false
	}
	switch attempt.AttemptState {
	case domain.ForwardingAttemptStateNotSent,
		domain.ForwardingAttemptStateResponseReceived:
		return true
	case domain.ForwardingAttemptStateSentNoResponse:
		return false
	default:
		return false
	}
}

func forwardingCapacityReservationID(
	localRequestID string,
	attemptNumber int,
) string {
	return fmt.Sprintf(
		"%s:attempt:%d",
		localRequestID,
		attemptNumber,
	)
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

func forwardingFailureRetryAfter(
	err error,
) (
	present bool,
	delay time.Duration,
	at time.Time,
	contractErr error,
) {
	var metadata ForwardingRetryAfter
	if !errors.As(err, &metadata) {
		return false, 0, time.Time{}, nil
	}

	present = metadata.FailureRetryAfterPresent()
	delay = metadata.FailureRetryAfterDelay()
	at = metadata.FailureRetryAfterTime()

	if !present {
		if delay != 0 || !at.IsZero() {
			return false, 0, time.Time{}, fmt.Errorf(
				"%w: retry-after values without presence",
				ErrStageContractViolation,
			)
		}
		return false, 0, time.Time{}, nil
	}
	if delay < 0 || (delay != 0 && !at.IsZero()) {
		return false, 0, time.Time{}, fmt.Errorf(
			"%w: invalid retry-after values",
			ErrStageContractViolation,
		)
	}
	if !at.IsZero() && at.Location() != time.UTC {
		return false, 0, time.Time{}, fmt.Errorf(
			"%w: retry-after time must be UTC",
			ErrStageContractViolation,
		)
	}
	return present, delay, at, nil
}

func (s *ForwardingStage) retryDelay(
	result leasedCandidateResult,
	attemptNumber int,
) (time.Duration, error) {
	if result.RetryAfterPresent {
		delay := result.RetryAfterDelay
		if !result.RetryAfterAt.IsZero() {
			now, err := forwardingStageNow(s.clock)
			if err != nil {
				return 0, err
			}
			delay = result.RetryAfterAt.Sub(now)
			if delay < 0 {
				delay = 0
			}
		}

		maximum := s.policy.UpstreamMaxBackoff()
		if result.FailureKind == "rate_limited" &&
			s.policy.RateLimitMaxWait() < maximum {
			maximum = s.policy.RateLimitMaxWait()
		}
		if delay > maximum {
			delay = maximum
		}
		return delay, nil
	}

	return boundedExponentialBackoff(
		s.policy.UpstreamMaxAttempts(),
		attemptNumber,
		s.policy.UpstreamMaxBackoff(),
	)
}

func boundedExponentialBackoff(
	maxAttempts int,
	attemptNumber int,
	maximum time.Duration,
) (time.Duration, error) {
	if maxAttempts < 2 ||
		attemptNumber < 1 ||
		attemptNumber >= maxAttempts ||
		maximum <= 0 {
		return 0, fmt.Errorf(
			"%w: invalid backoff input",
			ErrStageContractViolation,
		)
	}

	delay := maximum
	for remaining := maxAttempts - attemptNumber - 1; remaining > 0; remaining-- {
		delay /= 2
	}
	if delay <= 0 {
		delay = time.Nanosecond
	}
	return delay, nil
}
