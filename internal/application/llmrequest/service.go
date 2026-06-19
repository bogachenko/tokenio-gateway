package llmrequest

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

const localRequestIDPrefix = "llmreq_"

type Service struct {
	authenticator      Authenticator
	requestParser      RequestParser
	capabilityDetector CapabilityDetector
	routePlanner       RoutePlanner
	billingAdmitter    BillingAdmitter
	forwarding         ForwardingStageExecutor
	usageResolver      UsageResolver
	finalizer          Finalizer
	autoCharger        AutoCharger
}

func NewService(dependencies Dependencies) (*Service, error) {
	if dependencies.Authenticator == nil ||
		dependencies.RequestParser == nil ||
		dependencies.CapabilityDetector == nil ||
		dependencies.RoutePlanner == nil ||
		dependencies.BillingAdmitter == nil ||
		dependencies.Forwarding == nil ||
		dependencies.UsageResolver == nil ||
		dependencies.Finalizer == nil ||
		dependencies.AutoCharger == nil {
		return nil, ErrDependencyRequired
	}

	return &Service{
		authenticator:      dependencies.Authenticator,
		requestParser:      dependencies.RequestParser,
		capabilityDetector: dependencies.CapabilityDetector,
		routePlanner:       dependencies.RoutePlanner,
		billingAdmitter:    dependencies.BillingAdmitter,
		forwarding:         dependencies.Forwarding,
		usageResolver:      dependencies.UsageResolver,
		finalizer:          dependencies.Finalizer,
		autoCharger:        dependencies.AutoCharger,
	}, nil
}

func (s *Service) Execute(
	ctx context.Context,
	input Input,
) (ForwardedRequest, error) {
	if s == nil ||
		s.authenticator == nil ||
		s.requestParser == nil ||
		s.capabilityDetector == nil ||
		s.routePlanner == nil ||
		s.billingAdmitter == nil ||
		s.forwarding == nil ||
		s.usageResolver == nil ||
		s.finalizer == nil ||
		s.autoCharger == nil {
		return ForwardedRequest{}, ErrDependencyRequired
	}
	if ctx == nil {
		return ForwardedRequest{}, fmt.Errorf(
			"%w: nil context",
			ErrInvalidInput,
		)
	}

	prepared, err := s.prepare(ctx, input)
	if err != nil {
		return ForwardedRequest{}, err
	}

	admission, err := s.billingAdmitter.Admit(
		ctx,
		BillingAdmissionInput{
			Principal:            prepared.Principal,
			RequiredReserveCents: prepared.Plan.EstimatedClientAmountCents,
			Currency:             prepared.Plan.Currency,
		},
	)
	if err != nil {
		return ForwardedRequest{}, fmt.Errorf(
			"admit billing reserve: %w",
			err,
		)
	}
	if err := validateBillingAdmission(prepared, admission); err != nil {
		return ForwardedRequest{}, err
	}

	forwarded, err := s.forwarding.Execute(
		ctx,
		clonePreparedRequest(prepared),
		admission,
	)
	if err != nil {
		return ForwardedRequest{}, fmt.Errorf(
			"execute forwarding stage: %w",
			normalizeForwardingError(err),
		)
	}

	resolved, err := s.usageResolver.Resolve(
		ctx,
		UsageResolutionInput{
			Reserved: forwarded.Reserved,
			Response: cloneForwardResponse(
				forwarded.Response,
			),
		},
	)
	if err != nil {
		failed, failureErr := s.markPricingFailed(
			ctx,
			forwarded.Reserved,
			"usage_resolution_failed",
		)
		if failureErr != nil {
			return ForwardedRequest{}, errors.Join(
				fmt.Errorf("resolve final usage: %w", err),
				failureErr,
			)
		}
		forwarded.FinalUsageRecord = failed.Usage
		return forwarded, nil
	}
	if err := validateUsageResolution(
		forwarded,
		resolved,
	); err != nil {
		failed, failureErr := s.markPricingFailed(
			ctx,
			forwarded.Reserved,
			"usage_resolution_invalid",
		)
		if failureErr != nil {
			return ForwardedRequest{}, errors.Join(
				err,
				failureErr,
			)
		}
		forwarded.FinalUsageRecord = failed.Usage
		return forwarded, nil
	}

	finalized, err := s.finalizer.Commit(
		ctx,
		FinalizationInput{
			Reserved:      forwarded.Reserved,
			ResolvedUsage: resolved,
		},
	)
	if err != nil {
		return ForwardedRequest{}, fmt.Errorf(
			"finalize billable usage: %w",
			err,
		)
	}
	if finalized.Usage.Status != domain.UsageStatusBillable ||
		finalized.Usage.LocalRequestID !=
			forwarded.Reserved.Prepared.LocalRequestID {
		return ForwardedRequest{}, fmt.Errorf(
			"%w: invalid finalization result",
			ErrStageContractViolation,
		)
	}

	forwarded.ResolvedUsage = resolved
	forwarded.FinalUsageRecord = finalized.Usage
	forwarded.AutoCharge = cloneAutoChargeResult(
		s.autoCharger.Run(
			ctx,
			AutoChargeInput{
				Principal: forwarded.Reserved.
					Prepared.Principal,
				FinalUsageRecord: finalized.Usage,
			},
		),
	)
	return forwarded, nil
}

func normalizeForwardingError(err error) error {
	if err == nil {
		return nil
	}
	var failure ForwardingFailure
	if !errors.As(err, &failure) ||
		failure == nil ||
		!errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return upstreamTimeoutError(err)
}

func cloneAutoChargeResult(
	value AutoChargeResult,
) AutoChargeResult {
	result := value
	result.ProcessedBatchIDs = append(
		[]string(nil),
		value.ProcessedBatchIDs...,
	)
	if value.BillingBalanceCents != nil {
		copied := *value.BillingBalanceCents
		result.BillingBalanceCents = &copied
	}
	switch result.Status {
	case AutoChargeStatusDeferred,
		AutoChargeStatusProcessed,
		AutoChargeStatusFailed:
	default:
		result = AutoChargeResult{
			Status: AutoChargeStatusFailed,
		}
	}
	if result.ChargedAmountCents < 0 {
		result = AutoChargeResult{
			Status: AutoChargeStatusFailed,
		}
	}
	return result
}

func (s *Service) markPricingFailed(
	ctx context.Context,
	reserved ReservedRequest,
	reason string,
) (FinalizationResult, error) {
	finalized, err := s.finalizer.MarkPricingFailed(
		ctx,
		PricingFailureInput{
			Reserved:      reserved,
			FailureReason: reason,
		},
	)
	if err != nil {
		return FinalizationResult{}, fmt.Errorf(
			"finalize pricing failure: %w",
			err,
		)
	}
	if finalized.Usage.Status !=
		domain.UsageStatusPricingFailed ||
		finalized.Usage.LocalRequestID !=
			reserved.Prepared.LocalRequestID {
		return FinalizationResult{}, fmt.Errorf(
			"%w: invalid pricing failure result",
			ErrStageContractViolation,
		)
	}
	return finalized, nil
}

func validateUsageResolution(
	forwarded ForwardedRequest,
	resolved UsageResolutionResult,
) error {
	if resolved.Completeness == "" ||
		resolved.UpstreamCostCents < 0 ||
		resolved.ClientAmountCents < 0 ||
		(hasPositiveUsage(resolved.Usage) &&
			(resolved.UpstreamCostCents == 0 ||
				resolved.ClientAmountCents == 0)) ||
		resolved.Currency != forwarded.Reserved.Prepared.Plan.Currency ||
		!nonNegativeUsage(resolved.Usage) {
		return fmt.Errorf(
			"%w: invalid usage resolution",
			ErrStageContractViolation,
		)
	}
	return nil
}

func (s *Service) prepare(
	ctx context.Context,
	input Input,
) (PreparedRequest, error) {
	if err := validateInput(input); err != nil {
		return PreparedRequest{}, err
	}

	originalPayload := cloneBytes(input.Payload)

	principal, err := s.authenticator.Authenticate(
		ctx,
		input.RawAPIKey,
	)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf(
			"authenticate request: %w",
			err,
		)
	}
	if err := validatePrincipal(principal); err != nil {
		return PreparedRequest{}, err
	}

	parsed, err := s.requestParser.Parse(
		ctx,
		ParseInput{
			APIFamily:    input.APIFamily,
			EndpointKind: input.EndpointKind,
			PathModel:    input.PathModel,
			Payload:      cloneBytes(originalPayload),
		},
	)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf(
			"parse request: %w",
			err,
		)
	}
	if strings.TrimSpace(parsed.ClientModel) == "" {
		return PreparedRequest{}, fmt.Errorf(
			"%w: parser returned blank client model",
			ErrStageContractViolation,
		)
	}

	requestedCapabilities, err := s.capabilityDetector.Detect(
		ctx,
		CapabilityInput{
			APIFamily:    input.APIFamily,
			EndpointKind: input.EndpointKind,
			PathModel:    input.PathModel,
			ClientModel:  parsed.ClientModel,
			Payload:      cloneBytes(originalPayload),
		},
	)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf(
			"detect requested capabilities: %w",
			err,
		)
	}

	plan, err := s.routePlanner.Plan(
		ctx,
		RoutePlanInput{
			LocalRequestID:        input.LocalRequestID,
			Principal:             principal,
			APIFamily:             input.APIFamily,
			EndpointKind:          input.EndpointKind,
			ClientModel:           parsed.ClientModel,
			RequestedCapabilities: requestedCapabilities,
			Payload:               cloneBytes(originalPayload),
		},
	)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf(
			"plan route: %w",
			err,
		)
	}
	if err := validateRoutePlan(
		input,
		parsed,
		plan,
	); err != nil {
		return PreparedRequest{}, err
	}

	return PreparedRequest{
		LocalRequestID:        input.LocalRequestID,
		IdempotencyKey:        cloneStringPointer(input.IdempotencyKey),
		Principal:             principal,
		APIFamily:             input.APIFamily,
		EndpointKind:          input.EndpointKind,
		ClientModel:           parsed.ClientModel,
		RequestedCapabilities: requestedCapabilities,
		Payload:               cloneBytes(originalPayload),
		Plan:                  plan,
	}, nil
}

func validateInput(input Input) error {
	if !validLocalRequestID(input.LocalRequestID) ||
		strings.TrimSpace(input.RawAPIKey) == "" ||
		input.APIFamily == "" ||
		input.EndpointKind == "" ||
		input.Payload == nil {
		return ErrInvalidInput
	}
	if input.IdempotencyKey != nil &&
		strings.TrimSpace(*input.IdempotencyKey) == "" {
		return fmt.Errorf(
			"%w: blank idempotency key",
			ErrInvalidInput,
		)
	}
	return nil
}

func validatePrincipal(value Principal) error {
	if strings.TrimSpace(value.UserID) == "" ||
		strings.TrimSpace(value.APIKeyID) == "" ||
		strings.TrimSpace(value.BillingSubjectUserID) == "" {
		return fmt.Errorf(
			"%w: invalid authentication principal",
			ErrStageContractViolation,
		)
	}
	return nil
}

func validateRoutePlan(
	input Input,
	parsed ParsedRequest,
	plan RoutePlan,
) error {
	route := plan.Route
	reseller := plan.Reseller
	price := plan.Price

	if strings.TrimSpace(route.ID) == "" ||
		strings.TrimSpace(route.ResellerID) == "" ||
		strings.TrimSpace(route.ClientModel) == "" ||
		strings.TrimSpace(route.ProviderModel) == "" ||
		strings.TrimSpace(reseller.ID) == "" ||
		strings.TrimSpace(price.RouteID) == "" ||
		strings.TrimSpace(plan.BillingModel) == "" ||
		route.APIFamily != input.APIFamily ||
		route.EndpointKind != input.EndpointKind ||
		route.ClientModel != parsed.ClientModel ||
		route.ResellerID != reseller.ID ||
		route.ProviderType == "" ||
		route.ProviderType != reseller.ProviderType ||
		price.RouteID != route.ID ||
		!route.Enabled ||
		!reseller.Enabled ||
		!price.Enabled ||
		price.Currency != "RUB" ||
		plan.Currency != "RUB" ||
		plan.EstimatedClientAmountCents < 0 ||
		plan.EstimatedUpstreamCostCents < 0 ||
		strings.TrimSpace(plan.Confidence) == "" ||
		!nonNegativeUsage(plan.EstimatedUsage) {
		return fmt.Errorf(
			"%w: invalid route plan",
			ErrStageContractViolation,
		)
	}
	return nil
}

func validateBillingAdmission(
	prepared PreparedRequest,
	admission BillingAdmissionResult,
) error {
	if !admission.Allowed ||
		admission.Currency != prepared.Plan.Currency ||
		admission.RequiredReserveCents !=
			prepared.Plan.EstimatedClientAmountCents ||
		admission.RemoteBalanceCents < 0 ||
		admission.PendingAmountCents < 0 ||
		admission.RemoteBalanceCents <
			admission.PendingAmountCents ||
		admission.EffectiveBalanceCents !=
			admission.RemoteBalanceCents-
				admission.PendingAmountCents ||
		admission.EffectiveBalanceCents <
			admission.RequiredReserveCents {
		return fmt.Errorf(
			"%w: invalid billing admission",
			ErrStageContractViolation,
		)
	}
	return nil
}

func reservationInput(
	prepared PreparedRequest,
) ReservationInput {
	return ReservationInput{
		LocalRequestID: prepared.LocalRequestID,
		IdempotencyKey: cloneStringPointer(
			prepared.IdempotencyKey,
		),
		Principal:      prepared.Principal,
		APIFamily:      prepared.APIFamily,
		EndpointKind:   prepared.EndpointKind,
		ClientModel:    prepared.ClientModel,
		BillingModel:   prepared.Plan.BillingModel,
		Route:          prepared.Plan.Route,
		Reseller:       prepared.Plan.Reseller,
		EstimatedUsage: prepared.Plan.EstimatedUsage,
		EstimatedClientAmountCents: prepared.Plan.
			EstimatedClientAmountCents,
		EstimatedUpstreamCostCents: prepared.Plan.
			EstimatedUpstreamCostCents,
		Currency: prepared.Plan.Currency,
	}
}

func validateReservation(
	prepared PreparedRequest,
	result ReservationResult,
) error {
	usage := result.Usage
	expectedIdempotencyKey := ""
	if prepared.IdempotencyKey != nil {
		expectedIdempotencyKey = *prepared.IdempotencyKey
	}

	if result.Disposition != ReservationDispositionCreated &&
		result.Disposition !=
			ReservationDispositionAlreadyReserved ||
		usage.LocalRequestID != prepared.LocalRequestID ||
		usage.IdempotencyKey != expectedIdempotencyKey ||
		usage.UserID != prepared.Principal.UserID ||
		usage.APIKeyID != prepared.Principal.APIKeyID ||
		usage.APIFamily != prepared.APIFamily ||
		usage.EndpointKind != prepared.EndpointKind ||
		usage.ClientModel != prepared.ClientModel ||
		usage.BillingModel != prepared.Plan.BillingModel ||
		usage.SelectedRouteID != prepared.Plan.Route.ID ||
		usage.SelectedResellerID != prepared.Plan.Reseller.ID ||
		usage.ProviderType != prepared.Plan.Route.ProviderType ||
		usage.ProviderModel != prepared.Plan.Route.ProviderModel ||
		usage.EstimatedUsage != prepared.Plan.EstimatedUsage ||
		usage.EstimatedClientAmountCents !=
			prepared.Plan.EstimatedClientAmountCents ||
		usage.EstimatedUpstreamCostCents !=
			prepared.Plan.EstimatedUpstreamCostCents ||
		usage.Currency != prepared.Plan.Currency ||
		usage.UsageCompleteness != "missing" ||
		usage.Status != domain.UsageStatusReserved ||
		usage.Usage != (domain.TokenUsage{}) ||
		usage.ClientAmountCents != 0 ||
		usage.ChargedAmountCents != 0 ||
		usage.RemainingAmountCents != 0 ||
		usage.ActualUpstreamCostCents != 0 ||
		usage.ReservedAt == nil ||
		usage.ReleasedAt != nil ||
		usage.BillableAt != nil ||
		usage.ChargedAt != nil ||
		usage.FailedAt != nil ||
		usage.CreatedAt.IsZero() ||
		usage.UpdatedAt.IsZero() ||
		usage.CreatedAt.Location() != time.UTC ||
		usage.ReservedAt.Location() != time.UTC ||
		usage.UpdatedAt.Location() != time.UTC ||
		!usage.CreatedAt.Equal(*usage.ReservedAt) ||
		!usage.UpdatedAt.Equal(*usage.ReservedAt) ||
		strings.TrimSpace(usage.FailureReason) != "" ||
		strings.TrimSpace(usage.BillingChargeRequestID) != "" {
		return fmt.Errorf(
			"%w: invalid atomic reservation",
			ErrStageContractViolation,
		)
	}

	switch result.Disposition {
	case ReservationDispositionCreated:
		if result.Reseller == nil ||
			result.Reseller.ID != prepared.Plan.Reseller.ID ||
			result.Reseller.ProviderType !=
				prepared.Plan.Reseller.ProviderType ||
			result.Reseller.ReservedCents <
				prepared.Plan.EstimatedUpstreamCostCents {
			return fmt.Errorf(
				"%w: invalid created reseller reserve",
				ErrStageContractViolation,
			)
		}
	case ReservationDispositionAlreadyReserved:
		if result.Reseller != nil {
			return fmt.Errorf(
				"%w: replay returned mutable reseller snapshot",
				ErrStageContractViolation,
			)
		}
	}
	return nil
}

func validLocalRequestID(value string) bool {
	if !strings.HasPrefix(value, localRequestIDPrefix) ||
		len(value) == len(localRequestIDPrefix) {
		return false
	}
	for _, current := range value {
		if unicode.IsControl(current) ||
			unicode.IsSpace(current) {
			return false
		}
	}
	return true
}

func hasPositiveUsage(value domain.TokenUsage) bool {
	return value.InputTokens > 0 ||
		value.CachedInputTokens > 0 ||
		value.OutputTokens > 0 ||
		value.ReasoningTokens > 0 ||
		value.ImageInputTokens > 0 ||
		value.AudioInputTokens > 0 ||
		value.AudioOutputTokens > 0 ||
		value.FileInputTokens > 0 ||
		value.VideoInputTokens > 0 ||
		value.ImageGenerationUnits > 0
}

func nonNegativeUsage(value domain.TokenUsage) bool {
	return value.InputTokens >= 0 &&
		value.CachedInputTokens >= 0 &&
		value.OutputTokens >= 0 &&
		value.ReasoningTokens >= 0 &&
		value.ImageInputTokens >= 0 &&
		value.AudioInputTokens >= 0 &&
		value.AudioOutputTokens >= 0 &&
		value.FileInputTokens >= 0 &&
		value.VideoInputTokens >= 0 &&
		value.ImageGenerationUnits >= 0
}

func clonePreparedRequest(
	value PreparedRequest,
) PreparedRequest {
	result := value
	result.IdempotencyKey = cloneStringPointer(
		value.IdempotencyKey,
	)
	result.Payload = cloneBytes(value.Payload)
	result.Plan.Fallbacks = cloneRouteFallbackPlans(
		value.Plan.Fallbacks,
	)
	return result
}

func cloneRouteFallbackPlans(
	values []RouteFallbackPlan,
) []RouteFallbackPlan {
	if values == nil {
		return nil
	}
	return append([]RouteFallbackPlan(nil), values...)
}

func cloneReservationResult(
	value ReservationResult,
) ReservationResult {
	result := value
	result.Usage.ReservedAt = cloneTimePointer(
		value.Usage.ReservedAt,
	)
	result.Usage.ReleasedAt = cloneTimePointer(
		value.Usage.ReleasedAt,
	)
	result.Usage.BillableAt = cloneTimePointer(
		value.Usage.BillableAt,
	)
	result.Usage.ChargedAt = cloneTimePointer(
		value.Usage.ChargedAt,
	)
	result.Usage.FailedAt = cloneTimePointer(
		value.Usage.FailedAt,
	)
	if value.Reseller != nil {
		reseller := *value.Reseller
		reseller.DisabledAt = cloneTimePointer(
			value.Reseller.DisabledAt,
		)
		result.Reseller = &reseller
	}
	return result
}

func cloneBytes(value []byte) []byte {
	if value == nil {
		return nil
	}
	return append([]byte(nil), value...)
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}

func cloneTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	copyValue := *value
	return &copyValue
}
