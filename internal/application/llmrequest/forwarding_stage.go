package llmrequest

import (
	"context"
	"errors"
	"fmt"

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

type ForwardingStage struct {
	capacity    ports.RouteCapacityManager
	reservation AtomicReservation
	forwarder   ForwardingExecutor
}

type ForwardedRequest struct {
	Reserved      ReservedRequest
	Response      ports.ForwardResponse
	ResolvedUsage UsageResolutionResult
}

func NewForwardingStage(
	capacity ports.RouteCapacityManager,
	reservation AtomicReservation,
	forwarder ForwardingExecutor,
) (*ForwardingStage, error) {
	if capacity == nil ||
		reservation == nil ||
		forwarder == nil {
		return nil, ErrDependencyRequired
	}
	return &ForwardingStage{
		capacity:    capacity,
		reservation: reservation,
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

	execution, err := s.forwarder.Forward(
		ctx,
		ForwardingExecutionInput{
			Prepared:    clonePreparedRequest(prepared),
			Admission:   admission,
			Reservation: cloneReservationResult(reservation),
		},
	)
	if err != nil {
		return ForwardedRequest{}, fmt.Errorf(
			"forward selected route: %w",
			err,
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
