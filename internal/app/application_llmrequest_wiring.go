package app

import (
	"context"
	"errors"

	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	billingapp "github.com/bogachenko/tokenio-gateway/internal/application/billing"
	ledgerapp "github.com/bogachenko/tokenio-gateway/internal/application/ledger"
	llmrequest "github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	pricingapp "github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type LLMRequestAuthenticator = llmrequest.LLMRequestAuthenticator
type LLMRequestBillingAdmitter = llmrequest.LLMRequestBillingAdmitter
type LLMRequestAutoCharger = llmrequest.LLMRequestAutoCharger
type LLMRequestFinalizer = llmrequest.LLMRequestFinalizer
type LLMRequestForwardingExecutor = llmrequest.LLMRequestForwardingExecutor
type LLMRequestRoutePreflighter = llmrequest.LLMRequestRoutePreflighter
type LLMRequestRouteSelector = llmrequest.LLMRequestRouteSelector
type LLMRequestUsageResolver = llmrequest.LLMRequestUsageResolver

type publicAuthenticationUseCase interface {
	AuthenticatePublicRequest(
		context.Context,
		authenticateapp.Input,
	) (authenticateapp.Result, error)
}

type llmRequestPublicAuthenticationAdapter struct {
	usecase publicAuthenticationUseCase
}

func NewLLMRequestAuthenticator(
	public publicAuthenticationUseCase,
) (*llmrequest.LLMRequestAuthenticator, error) {
	if public == nil {
		return nil, llmrequest.ErrDependencyRequired
	}
	return llmrequest.NewLLMRequestAuthenticator(
		llmRequestPublicAuthenticationAdapter{usecase: public},
	)
}

func (a llmRequestPublicAuthenticationAdapter) AuthenticatePublicRequest(
	ctx context.Context,
	rawAPIKey string,
) (llmrequest.Principal, error) {
	result, err := a.usecase.AuthenticatePublicRequest(
		ctx,
		authenticateapp.Input{RawAPIKey: rawAPIKey},
	)
	if err != nil {
		return llmrequest.Principal{}, err
	}
	return llmrequest.Principal{
		UserID:               result.Principal.UserID,
		APIKeyID:             result.Principal.APIKeyID,
		BillingSubjectUserID: result.Principal.BillingSubjectUserID,
	}, nil
}

type billingAdmissionUseCase interface {
	Admit(
		context.Context,
		billingapp.AdmissionInput,
	) (billingapp.AdmissionResult, error)
}

type llmRequestBillingAdmissionAdapter struct {
	usecase billingAdmissionUseCase
}

func NewLLMRequestBillingAdmitter(
	billing billingAdmissionUseCase,
) (*llmrequest.LLMRequestBillingAdmitter, error) {
	if billing == nil {
		return nil, llmrequest.ErrDependencyRequired
	}
	return llmrequest.NewLLMRequestBillingAdmitter(
		llmRequestBillingAdmissionAdapter{usecase: billing},
	)
}

func (a llmRequestBillingAdmissionAdapter) Admit(
	ctx context.Context,
	input llmrequest.BillingAdmissionInput,
) (llmrequest.BillingAdmissionResult, error) {
	result, err := a.usecase.Admit(
		ctx,
		billingapp.AdmissionInput{
			UserID:               input.Principal.UserID,
			BillingSubjectUserID: input.Principal.BillingSubjectUserID,
			RequiredReserveCents: input.RequiredReserveCents,
			Currency:             input.Currency,
		},
	)
	if err != nil {
		return llmrequest.BillingAdmissionResult{}, err
	}
	return llmrequest.BillingAdmissionResult{
		Allowed:               result.Allowed,
		RemoteBalanceCents:    result.RemoteBalanceCents,
		PendingAmountCents:    result.PendingAmountCents,
		EffectiveBalanceCents: result.EffectiveBalanceCents,
		RequiredReserveCents:  result.RequiredReserveCents,
		Currency:              result.Currency,
	}, nil
}

type autoChargeUseCase interface {
	Run(
		context.Context,
		billingapp.AutoChargeInput,
	) (billingapp.AutoChargeResult, error)
}

type llmRequestAutoChargeAdapter struct {
	usecase autoChargeUseCase
}

func NewLLMRequestAutoCharger(
	service autoChargeUseCase,
) (*llmrequest.LLMRequestAutoCharger, error) {
	if service == nil {
		return nil, llmrequest.ErrDependencyRequired
	}
	return llmrequest.NewLLMRequestAutoCharger(
		llmRequestAutoChargeAdapter{usecase: service},
	)
}

func (a llmRequestAutoChargeAdapter) Run(
	ctx context.Context,
	input llmrequest.AutoChargeServiceInput,
) (llmrequest.AutoChargeServiceResult, error) {
	result, err := a.usecase.Run(
		ctx,
		billingapp.AutoChargeInput{
			UserID:               input.UserID,
			BillingSubjectUserID: input.BillingSubjectUserID,
			Currency:             input.Currency,
		},
	)
	if errors.Is(err, billingapp.ErrChargeDeferred) {
		return llmrequest.AutoChargeServiceResult{Deferred: true}, nil
	}
	if err != nil {
		return llmrequest.AutoChargeServiceResult{}, err
	}
	return llmrequest.AutoChargeServiceResult{
		Deferred:            result.Deferred,
		ProcessedBatchIDs:   append([]string(nil), result.ProcessedBatchIDs...),
		ChargedAmountCents:  result.ChargedAmountCents,
		BillingBalanceCents: copyInt64Pointer(result.BillingBalanceCents),
	}, nil
}

type llmRequestLedgerUseCase interface {
	CommitBillable(
		context.Context,
		ledgerapp.CommitBillableInput,
	) (domain.UsageRecord, error)
	MarkPricingFailed(
		context.Context,
		ledgerapp.MarkPricingFailedInput,
	) (domain.UsageRecord, error)
}

type llmRequestLedgerAdapter struct {
	usecase llmRequestLedgerUseCase
}

func NewLLMRequestFinalizer(
	ledger llmRequestLedgerUseCase,
) (*llmrequest.LLMRequestFinalizer, error) {
	if ledger == nil {
		return nil, llmrequest.ErrDependencyRequired
	}
	return llmrequest.NewLLMRequestFinalizer(
		llmRequestLedgerAdapter{usecase: ledger},
	)
}

func (a llmRequestLedgerAdapter) CommitBillable(
	ctx context.Context,
	input llmrequest.CommitBillableInput,
) (domain.UsageRecord, error) {
	return a.usecase.CommitBillable(
		ctx,
		ledgerapp.CommitBillableInput{
			LocalRequestID:          input.LocalRequestID,
			Usage:                   input.Usage,
			UsageCompleteness:       input.UsageCompleteness,
			ClientAmountCents:       input.ClientAmountCents,
			ActualUpstreamCostCents: input.ActualUpstreamCostCents,
			ProviderRequestID:       input.ProviderRequestID,
			ProviderResponseModel:   input.ProviderResponseModel,
		},
	)
}

func (a llmRequestLedgerAdapter) MarkPricingFailed(
	ctx context.Context,
	input llmrequest.MarkPricingFailedInput,
) (domain.UsageRecord, error) {
	return a.usecase.MarkPricingFailed(
		ctx,
		ledgerapp.MarkPricingFailedInput{
			LocalRequestID:    input.LocalRequestID,
			Usage:             input.Usage,
			UsageCompleteness: input.UsageCompleteness,
			FailureReason:     input.FailureReason,
		},
	)
}

type llmRequestPreflightPricerAdapter struct {
	pricer *pricingapp.PreflightPricer
}

func NewLLMRequestRoutePreflighter(
	secrets ports.SecretPresenceChecker,
	pricer *pricingapp.PreflightPricer,
	capacity ports.RouteCapacityChecker,
	adapterSupport ports.ForwardingAdapterSupport,
	rewriteSupport ports.ModelIdentifierRewriteSupport,
) (*llmrequest.LLMRequestRoutePreflighter, error) {
	if pricer == nil {
		return nil, llmrequest.ErrDependencyRequired
	}
	return llmrequest.NewLLMRequestRoutePreflighter(
		secrets,
		llmRequestPreflightPricerAdapter{pricer: pricer},
		capacity,
		adapterSupport,
		rewriteSupport,
	)
}

func (a llmRequestPreflightPricerAdapter) Price(
	ctx context.Context,
	input llmrequest.PreflightPricingInput,
) (llmrequest.PreflightPricingResult, error) {
	result, err := a.pricer.Price(
		ctx,
		pricingapp.PreflightInput{
			Route:                 input.Route,
			Price:                 input.Price,
			RequestBody:           append([]byte(nil), input.RequestBody...),
			RequestedCapabilities: input.RequestedCapabilities,
		},
	)
	if err != nil {
		return llmrequest.PreflightPricingResult{}, err
	}
	return llmrequest.PreflightPricingResult{
		EstimatedUsage:             result.EstimatedUsage,
		EstimatedClientAmountCents: result.EstimatedClientAmountCents,
		EstimatedUpstreamCostCents: result.EstimatedUpstreamCostCents,
		Currency:                   result.Currency,
		Confidence:                 result.Confidence,
	}, nil
}

func NewLLMRequestRouteSelector(
	clock ports.Clock,
) (*llmrequest.LLMRequestRouteSelector, error) {
	return llmrequest.NewLLMRequestRouteSelector(clock)
}

type llmRequestUsagePricingResolverAdapter struct {
	resolver *pricingapp.UsageResolver
}

func NewLLMRequestUsageResolver(
	resolver *pricingapp.UsageResolver,
) (*llmrequest.LLMRequestUsageResolver, error) {
	if resolver == nil {
		return nil, llmrequest.ErrDependencyRequired
	}
	return llmrequest.NewLLMRequestUsageResolver(
		llmRequestUsagePricingResolverAdapter{resolver: resolver},
	)
}

func (a llmRequestUsagePricingResolverAdapter) Resolve(
	ctx context.Context,
	input llmrequest.UsagePricingInput,
) (llmrequest.UsagePricingResult, error) {
	result, err := a.resolver.Resolve(
		ctx,
		pricingapp.ResolveUsageInput{
			Route:        input.Route,
			Price:        input.Price,
			RequestBody:  append([]byte(nil), input.RequestBody...),
			ResponseBody: append([]byte(nil), input.ResponseBody...),
			ActualUsage:  copyTokenUsagePointer(input.ActualUsage),
			RequestedCapabilities: input.
				RequestedCapabilities,
			Modalities: pricingapp.InputModalities{
				Image: input.Modalities.Image,
				Audio: input.Modalities.Audio,
				File:  input.Modalities.File,
				Video: input.Modalities.Video,
			},
			ZeroUsageAllowed: input.ZeroUsageAllowed,
		},
	)
	if err != nil {
		return llmrequest.UsagePricingResult{}, err
	}
	return llmrequest.UsagePricingResult{
		Usage:                 result.Usage,
		Completeness:          result.Completeness,
		Estimated:             result.Estimated,
		UpstreamCostCents:     result.UpstreamCostCents,
		ClientAmountCents:     result.ClientAmountCents,
		Currency:              result.Currency,
		ProviderRequestID:     result.ProviderRequestID,
		ProviderResponseModel: result.ProviderResponseModel,
	}, nil
}

func NewLLMRequestForwardingExecutor(
	secrets ports.SecretResolver,
	factory ports.ForwardingAdapterFactory,
	maxResponseBodyBytes int64,
) (*llmrequest.LLMRequestForwardingExecutor, error) {
	return llmrequest.NewLLMRequestForwardingExecutor(
		secrets,
		factory,
		maxResponseBodyBytes,
	)
}

func copyInt64Pointer(value *int64) *int64 {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func copyTokenUsagePointer(value *domain.TokenUsage) *domain.TokenUsage {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}
