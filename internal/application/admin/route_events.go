package admin

import (
	"context"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type RouteEventListInput struct {
	RouteID        string
	ResellerID     string
	EventType      domain.RouteEventType
	LocalRequestID string
	CreatedFrom    *time.Time
	CreatedTo      *time.Time
	Limit          int
	Offset         int
}

func (s *Service) ListRouteEvents(
	ctx context.Context,
	input RouteEventListInput,
) (ListResult[domain.RouteEvent], error) {
	if ctx == nil || s == nil || s.deps.RouteEvents == nil {
		return ListResult[domain.RouteEvent]{}, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return ListResult[domain.RouteEvent]{}, err
	}
	page, err := normalizePage(input.Limit, input.Offset)
	if err != nil ||
		validateWindow(input.CreatedFrom, input.CreatedTo) != nil ||
		!validOptionalOpaque(input.RouteID) ||
		!validOptionalOpaque(input.ResellerID) ||
		!validOptionalOpaque(input.LocalRequestID) ||
		input.EventType != "" && !validRouteEventType(input.EventType) {
		return ListResult[domain.RouteEvent]{}, ErrInvalidRequest
	}
	stored, err := s.deps.RouteEvents.ListRouteEvents(
		ctx,
		ports.RouteEventListFilter{
			RouteID:        input.RouteID,
			ResellerID:     input.ResellerID,
			EventType:      input.EventType,
			LocalRequestID: input.LocalRequestID,
			CreatedFrom:    cloneAdminTime(input.CreatedFrom),
			CreatedTo:      cloneAdminTime(input.CreatedTo),
			Page:           page,
		},
	)
	if err != nil {
		return ListResult[domain.RouteEvent]{}, mapStoreError(err)
	}
	for _, event := range stored.Items {
		if isBlank(event.ID) || requireUTC(event.CreatedAt) != nil {
			return ListResult[domain.RouteEvent]{}, ErrStoreUnavailable
		}
	}
	return listResult(stored, page), nil
}

func validRouteEventType(value domain.RouteEventType) bool {
	switch value {
	case domain.RouteEventTypeSelected,
		domain.RouteEventTypeSkipped,
		domain.RouteEventTypeCapacityRejected,
		domain.RouteEventTypeForwardingStarted,
		domain.RouteEventTypeRetryScheduled,
		domain.RouteEventTypeForwardingSucceeded,
		domain.RouteEventTypeForwardingFailed,
		domain.RouteEventTypeCooldownSet,
		domain.RouteEventTypeCooldownExpired,
		domain.RouteEventTypeHealthcheckFailed,
		domain.RouteEventTypeHealthcheckRecovered,
		domain.RouteEventTypeBalanceLow:
		return true
	default:
		return false
	}
}
