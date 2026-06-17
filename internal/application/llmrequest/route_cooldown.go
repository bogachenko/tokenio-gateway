package llmrequest

import (
	"context"
	"fmt"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func cooldownDurationForFailureKind(policy RoutingPolicy, failureKind string) (time.Duration, bool) {
	switch failureKind {
	case "rate_limited":
		return policy.CooldownRateLimit(), true
	case "quota_exceeded":
		return policy.CooldownQuotaExceeded(), true
	case "provider_5xx":
		return policy.Cooldown5XX(), true
	case "timeout":
		return policy.CooldownTimeout(), true
	case "auth_error":
		return policy.CooldownAuthError(), true
	default:
		return 0, false
	}
}

func (stage *ForwardingStage) persistRouteCooldown(
	ctx context.Context,
	prepared PreparedRequest,
	attempt domain.ForwardingAttempt,
) error {
	duration, required := cooldownDurationForFailureKind(stage.policy, attempt.FailureKind)
	if !required {
		return nil
	}
	if attempt.CompletedAt == nil {
		return fmt.Errorf("%w: cooldown attempt has no completion time", ErrStageContractViolation)
	}
	at := *attempt.CompletedAt
	until := at.Add(duration)
	next := prepared.Plan.Route
	next.CooldownUntil = &until
	next.CooldownReason = attempt.FailureKind
	next.LastErrorCode = attempt.FailureKind
	next.LastErrorAt = &at
	next.UpdatedAt = at
	event := domain.RouteEvent{
		ID:      fmt.Sprintf("%s:attempt:%d:cooldown_set", prepared.LocalRequestID, attempt.AttemptNumber),
		RouteID: next.ID, ResellerID: next.ResellerID,
		ProviderType: next.ProviderType, APIFamily: next.APIFamily,
		EndpointKind: next.EndpointKind, ClientModel: next.ClientModel,
		EventType: domain.RouteEventTypeCooldownSet,
		Reason:    attempt.FailureKind, LocalRequestID: prepared.LocalRequestID,
		Metadata: domain.RouteEventMetadata{
			"attempt_number":       attempt.AttemptNumber,
			"failure_kind":         attempt.FailureKind,
			"upstream_status_code": attempt.UpstreamStatusCode,
			"cooldown_until":       until.Format(time.RFC3339Nano),
		},
		CreatedAt: at,
	}
	persisted, err := stage.cooldowns.CompareAndSwapRouteCooldownWithEvent(
		context.WithoutCancel(ctx), prepared.Plan.Route, next, event,
	)
	if err != nil {
		return fmt.Errorf("persist route cooldown: %w", err)
	}
	if persisted.ID != next.ID || persisted.CooldownUntil == nil ||
		!persisted.CooldownUntil.Equal(until) ||
		persisted.CooldownReason != attempt.FailureKind ||
		persisted.LastErrorCode != attempt.FailureKind ||
		persisted.LastErrorAt == nil || !persisted.LastErrorAt.Equal(at) {
		return fmt.Errorf("%w: invalid persisted route cooldown", ErrStageContractViolation)
	}
	return nil
}
