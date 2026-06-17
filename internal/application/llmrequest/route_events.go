package llmrequest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const (
	routeEventReasonSelectedCheapest = "selected_cheapest_available"
	routeEventReasonNotSelected      = "not_selected"
	routeEventReasonManualDisabled   = "manual_disabled"
	routeEventReasonMissingSecret    = "missing_reseller_api_key"
	routeEventReasonCooldownActive   = "cooldown_active"
	routeEventReasonInvalidPrice     = "invalid_route_price"
	routeEventReasonPricing          = "pricing_unavailable"
	routeEventReasonRateLimit        = "rate_limit_exceeded"
	routeEventReasonConcurrency      = "concurrency_limit_exceeded"
	routeEventReasonRewrite          = "unsupported_model_rewrite_policy"
	routeEventReasonAdapter          = "forwarding_adapter_unavailable"
	routeEventReasonCapacity         = "capacity_unavailable"
	routeEventReasonAttemptStarted   = "attempt_started"
	routeEventReasonRetryScheduled   = "safe_retry"
	routeEventReasonSucceeded        = "upstream_success"
	routeEventReasonCooldownElapsed  = "cooldown_elapsed"
)

func routeEvent(
	id string,
	eventType domain.RouteEventType,
	reason string,
	localRequestID string,
	route domain.Route,
	at time.Time,
	metadata domain.RouteEventMetadata,
) domain.RouteEvent {
	if metadata == nil {
		metadata = domain.RouteEventMetadata{}
	}
	return domain.RouteEvent{
		ID:             id,
		RouteID:        route.ID,
		ResellerID:     route.ResellerID,
		ProviderType:   route.ProviderType,
		APIFamily:      route.APIFamily,
		EndpointKind:   route.EndpointKind,
		ClientModel:    route.ClientModel,
		EventType:      eventType,
		Reason:         reason,
		LocalRequestID: localRequestID,
		Metadata:       metadata,
		CreatedAt:      at,
	}
}

func appendRouteEvent(
	ctx context.Context,
	store ports.RouteEventStore,
	event domain.RouteEvent,
) error {
	if ctx == nil || store == nil {
		return ErrDependencyRequired
	}
	if err := store.AppendRouteEvent(context.WithoutCancel(ctx), event); err != nil {
		return fmt.Errorf("append route event %q: %w", event.EventType, err)
	}
	return nil
}

func (planner *RepositoryRoutePlanner) expireCooldowns(
	ctx context.Context,
	input RoutePlanInput,
	routes []domain.Route,
) ([]domain.Route, error) {
	now, err := forwardingStageNow(planner.clock)
	if err != nil {
		return nil, err
	}
	result := append([]domain.Route(nil), routes...)
	for index, route := range result {
		if route.CooldownUntil == nil || route.CooldownUntil.After(now) {
			continue
		}
		previousUntil := *route.CooldownUntil
		previousReason := route.CooldownReason
		next := route
		next.CooldownUntil = nil
		next.CooldownReason = ""
		next.UpdatedAt = now

		event := routeEvent(
			fmt.Sprintf(
				"route:%s:cooldown:%d:expired",
				route.ID,
				previousUntil.UnixNano(),
			),
			domain.RouteEventTypeCooldownExpired,
			routeEventReasonCooldownElapsed,
			input.LocalRequestID,
			route,
			now,
			domain.RouteEventMetadata{
				"previous_cooldown_reason": previousReason,
				"previous_cooldown_until":  previousUntil.Format(time.RFC3339Nano),
			},
		)

		persisted, persistErr := planner.operational.
			CompareAndSwapRouteCooldownExpiryWithEvent(
				context.WithoutCancel(ctx),
				route,
				next,
				event,
			)
		if persistErr != nil {
			if errors.Is(persistErr, ports.ErrStoreConflict) {
				result[index] = next
				continue
			}
			return nil, fmt.Errorf(
				"expire route %q cooldown: %w",
				route.ID,
				persistErr,
			)
		}
		result[index] = persisted
	}
	return result, nil
}

func (planner *RepositoryRoutePlanner) recordSelectionEvents(
	ctx context.Context,
	input RoutePlanInput,
	candidates []RouteSelectionCandidate,
	selection RouteSelectionResult,
) error {
	now, err := forwardingStageNow(planner.clock)
	if err != nil {
		return err
	}

	eligible := make(map[string]struct{}, 1+len(selection.FallbackRouteIDs))
	if selection.SelectedRouteID != "" {
		eligible[selection.SelectedRouteID] = struct{}{}
	}
	for _, routeID := range selection.FallbackRouteIDs {
		eligible[routeID] = struct{}{}
	}

	for _, candidate := range candidates {
		if candidate.Route.ID == selection.SelectedRouteID {
			event := routeEvent(
				fmt.Sprintf(
					"%s:route:%s:selected",
					input.LocalRequestID,
					candidate.Route.ID,
				),
				domain.RouteEventTypeSelected,
				routeEventReasonSelectedCheapest,
				input.LocalRequestID,
				candidate.Route,
				now,
				domain.RouteEventMetadata{
					"estimated_upstream_cost_cents": candidate.Preflight.
						EstimatedUpstreamCostCents,
				},
			)
			if err := appendRouteEvent(ctx, planner.operational, event); err != nil {
				return err
			}
			continue
		}
		if _, isFallback := eligible[candidate.Route.ID]; isFallback {
			continue
		}
		event := routeEvent(
			fmt.Sprintf(
				"%s:route:%s:skipped",
				input.LocalRequestID,
				candidate.Route.ID,
			),
			domain.RouteEventTypeSkipped,
			routeCandidateSkipReason(candidate, now),
			input.LocalRequestID,
			candidate.Route,
			now,
			domain.RouteEventMetadata{},
		)
		if err := appendRouteEvent(ctx, planner.operational, event); err != nil {
			return err
		}
	}
	return nil
}

func routeCandidateSkipReason(
	candidate RouteSelectionCandidate,
	now time.Time,
) string {
	switch {
	case !candidate.Route.Enabled || !candidate.Reseller.Enabled:
		return routeEventReasonManualDisabled
	case !candidate.Preflight.SecretAvailable:
		return routeEventReasonMissingSecret
	case candidate.Route.CooldownUntil != nil &&
		candidate.Route.CooldownUntil.After(now):
		return routeEventReasonCooldownActive
	case candidate.Price == nil:
		return routeEventReasonInvalidPrice
	case !candidate.Preflight.CostAvailable:
		return routeEventReasonPricing
	case !candidate.Preflight.RateLimitAllowed:
		return routeEventReasonRateLimit
	case !candidate.Preflight.ConcurrencyAllowed:
		return routeEventReasonConcurrency
	case !candidate.Preflight.ModelIdentifierRewriteAllowed:
		return routeEventReasonRewrite
	case !candidate.Preflight.ForwardingAdapterAvailable:
		return routeEventReasonAdapter
	default:
		return routeEventReasonNotSelected
	}
}

func (stage *ForwardingStage) appendForwardingEvent(
	ctx context.Context,
	prepared PreparedRequest,
	attemptNumber int,
	eventType domain.RouteEventType,
	reason string,
	at time.Time,
	metadata domain.RouteEventMetadata,
) error {
	event := routeEvent(
		fmt.Sprintf(
			"%s:attempt:%d:%s",
			prepared.LocalRequestID,
			attemptNumber,
			eventType,
		),
		eventType,
		reason,
		prepared.LocalRequestID,
		prepared.Plan.Route,
		at,
		metadata,
	)
	return appendRouteEvent(ctx, stage.cooldowns, event)
}
