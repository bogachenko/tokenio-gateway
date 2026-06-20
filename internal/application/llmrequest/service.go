package llmrequest

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

func (s *Service) Execute(ctx context.Context, input Input) (ForwardedRequest, error) {
	if s == nil || s.authenticator == nil || s.requestParser == nil || s.capabilityDetector == nil || s.routePlanner == nil || s.billingAdmitter == nil || s.forwarding == nil || s.usageResolver == nil || s.finalizer == nil || s.autoCharger == nil {
		return ForwardedRequest{}, ErrDependencyRequired
	}
	if ctx == nil {
		return ForwardedRequest{}, fmt.Errorf("%w: nil context", ErrInvalidInput)
	}
	prepared, err := s.prepare(ctx, input)
	if err != nil {
		return ForwardedRequest{}, err
	}
	admission, err := s.billingAdmitter.Admit(ctx, BillingAdmissionInput{Principal: prepared.Principal, RequiredReserveCents: prepared.Plan.EstimatedClientAmountCents, Currency: prepared.Plan.Currency})
	if err != nil {
		return ForwardedRequest{}, fmt.Errorf("admit billing reserve: %w", err)
	}
	if err := validateBillingAdmission(prepared, admission); err != nil {
		return ForwardedRequest{}, err
	}
	forwarded, err := s.forwarding.Execute(ctx, clonePreparedRequest(prepared), admission)
	if err != nil {
		return ForwardedRequest{}, fmt.Errorf("execute forwarding stage: %w", normalizeForwardingError(err))
	}
	resolved, err := s.usageResolver.Resolve(ctx, UsageResolutionInput{Reserved: forwarded.Reserved, Response: cloneForwardResponse(forwarded.Response)})
	if err != nil {
		failed, failureErr := s.markPricingFailed(ctx, forwarded.Reserved, "usage_resolution_failed")
		if failureErr != nil {
			return ForwardedRequest{}, errors.Join(fmt.Errorf("resolve final usage: %w", err), failureErr)
		}
		forwarded.FinalUsageRecord = failed.Usage
		return forwarded, nil
	}
	if err := validateUsageResolution(forwarded, resolved); err != nil {
		failed, failureErr := s.markPricingFailed(ctx, forwarded.Reserved, "usage_resolution_invalid")
		if failureErr != nil {
			return ForwardedRequest{}, errors.Join(err, failureErr)
		}
		forwarded.FinalUsageRecord = failed.Usage
		return forwarded, nil
	}
	finalized, err := s.finalizer.Commit(ctx, FinalizationInput{Reserved: forwarded.Reserved, ResolvedUsage: resolved})
	if err != nil {
		return ForwardedRequest{}, fmt.Errorf("finalize billable usage: %w", err)
	}
	if finalized.Usage.Status != domain.UsageStatusBillable || finalized.Usage.LocalRequestID != forwarded.Reserved.Prepared.LocalRequestID {
		return ForwardedRequest{}, fmt.Errorf("%w: invalid finalization result", ErrStageContractViolation)
	}
	forwarded.ResolvedUsage = resolved
	forwarded.FinalUsageRecord = finalized.Usage
	forwarded.AutoCharge = cloneAutoChargeResult(s.autoCharger.Run(ctx, AutoChargeInput{Principal: forwarded.Reserved.Prepared.Principal, FinalUsageRecord: finalized.Usage}))
	return forwarded, nil
}

func normalizeForwardingError(err error) error {
	if err == nil {
		return nil
	}
	var failure ForwardingFailure
	if !errors.As(err, &failure) || failure == nil || !errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return upstreamTimeoutError(err)
}

func (s *Service) markPricingFailed(ctx context.Context, reserved ReservedRequest, reason string) (FinalizationResult, error) {
	finalized, err := s.finalizer.MarkPricingFailed(ctx, PricingFailureInput{Reserved: reserved, FailureReason: reason})
	if err != nil {
		return FinalizationResult{}, fmt.Errorf("finalize pricing failure: %w", err)
	}
	if finalized.Usage.Status != domain.UsageStatusPricingFailed || finalized.Usage.LocalRequestID != reserved.Prepared.LocalRequestID {
		return FinalizationResult{}, fmt.Errorf("%w: invalid pricing failure result", ErrStageContractViolation)
	}
	return finalized, nil
}

func validateUsageResolution(forwarded ForwardedRequest, resolved UsageResolutionResult) error {
	if resolved.Completeness == "" || resolved.UpstreamCostCents < 0 || resolved.ClientAmountCents < 0 || (hasPositiveUsage(resolved.Usage) && (resolved.UpstreamCostCents == 0 || resolved.ClientAmountCents == 0)) || resolved.Currency != forwarded.Reserved.Prepared.Plan.Currency || !nonNegativeUsage(resolved.Usage) {
		return fmt.Errorf("%w: invalid usage resolution", ErrStageContractViolation)
	}
	return nil
}

func (s *Service) prepare(ctx context.Context, input Input) (PreparedRequest, error) {
	if err := validateInput(input); err != nil {
		return PreparedRequest{}, err
	}
	originalPayload := cloneBytes(input.Payload)
	principal, err := s.authenticator.Authenticate(ctx, input.RawAPIKey)
	if err != nil {
		return PreparedRequest{}, fmt.Errorf("authenticate request: %w", err)
	}
	if err := validatePrincipal(principal); err != nil {
		return PreparedRequest{}, err
	}
	parsed, err := s.requestParser.Parse(ctx, ParseInput{APIFamily: input.APIFamily, EndpointKind: input.EndpointKind, PathModel: input.PathModel, Payload: cloneBytes(originalPayload)})
	if err != nil {
		return PreparedRequest{}, fmt.Errorf("parse request: %w", err)
	}
	if strings.TrimSpace(parsed.ClientModel) == "" {
		return PreparedRequest{}, fmt.Errorf("%w: parser returned blank client model", ErrStageContractViolation)
	}
	requestedCapabilities, err := s.capabilityDetector.Detect(ctx, CapabilityInput{APIFamily: input.APIFamily, EndpointKind: input.EndpointKind, PathModel: input.PathModel, ClientModel: parsed.ClientModel, Payload: cloneBytes(originalPayload)})
	if err != nil {
		return PreparedRequest{}, fmt.Errorf("detect requested capabilities: %w", err)
	}
	plan, err := s.routePlanner.Plan(ctx, RoutePlanInput{LocalRequestID: input.LocalRequestID, Principal: principal, APIFamily: input.APIFamily, EndpointKind: input.EndpointKind, ClientModel: parsed.ClientModel, RequestedCapabilities: requestedCapabilities, Payload: cloneBytes(originalPayload)})
	if err != nil {
		return PreparedRequest{}, fmt.Errorf("plan route: %w", err)
	}
	if err := validateRoutePlan(input, parsed, plan); err != nil {
		return PreparedRequest{}, err
	}
	return PreparedRequest{LocalRequestID: input.LocalRequestID, IdempotencyKey: cloneStringPointer(input.IdempotencyKey), Principal: principal, APIFamily: input.APIFamily, EndpointKind: input.EndpointKind, ClientModel: parsed.ClientModel, RequestedCapabilities: requestedCapabilities, UpstreamPath: input.UpstreamPath, Payload: cloneBytes(originalPayload), Plan: plan}, nil
}

func validateInput(input Input) error {
	if !validLocalRequestID(input.LocalRequestID) || strings.TrimSpace(input.RawAPIKey) == "" || input.APIFamily == "" || input.EndpointKind == "" || input.Payload == nil {
		return ErrInvalidInput
	}
	if input.IdempotencyKey != nil && strings.TrimSpace(*input.IdempotencyKey) == "" {
		return fmt.Errorf("%w: blank idempotency key", ErrInvalidInput)
	}
	return nil
}
