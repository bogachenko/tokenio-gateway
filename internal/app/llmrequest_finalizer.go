package app

import (
	"context"
	"fmt"

	ledgerapp "github.com/bogachenko/tokenio-gateway/internal/application/ledger"
	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	pricingapp "github.com/bogachenko/tokenio-gateway/internal/application/pricing"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type llmRequestLedger interface {
	CommitBillable(
		context.Context,
		ledgerapp.CommitBillableInput,
	) (domain.UsageRecord, error)
	MarkPricingFailed(
		context.Context,
		ledgerapp.MarkPricingFailedInput,
	) (domain.UsageRecord, error)
}

type LLMRequestFinalizer struct {
	ledger llmRequestLedger
}

var _ llmrequest.Finalizer = (*LLMRequestFinalizer)(nil)

func NewLLMRequestFinalizer(
	ledger llmRequestLedger,
) (*LLMRequestFinalizer, error) {
	if ledger == nil {
		return nil, llmrequest.ErrDependencyRequired
	}
	return &LLMRequestFinalizer{ledger: ledger}, nil
}

func (f *LLMRequestFinalizer) Commit(
	ctx context.Context,
	input llmrequest.FinalizationInput,
) (llmrequest.FinalizationResult, error) {
	if f == nil || f.ledger == nil {
		return llmrequest.FinalizationResult{},
			llmrequest.ErrDependencyRequired
	}
	completeness, err := pricingapp.ParseUsageCompleteness(
		input.ResolvedUsage.Completeness,
	)
	if err != nil {
		return llmrequest.FinalizationResult{}, fmt.Errorf(
			"parse final usage completeness: %w",
			err,
		)
	}
	record, err := f.ledger.CommitBillable(
		ctx,
		ledgerapp.CommitBillableInput{
			LocalRequestID:    input.Reserved.Prepared.LocalRequestID,
			Usage:             input.ResolvedUsage.Usage,
			UsageCompleteness: completeness,
			ClientAmountCents: input.ResolvedUsage.
				ClientAmountCents,
			ActualUpstreamCostCents: input.ResolvedUsage.
				UpstreamCostCents,
			ProviderRequestID: input.ResolvedUsage.
				ProviderRequestID,
			ProviderResponseModel: input.ResolvedUsage.
				ProviderResponseModel,
		},
	)
	if err != nil {
		return llmrequest.FinalizationResult{}, fmt.Errorf(
			"commit billable usage: %w",
			err,
		)
	}
	return llmrequest.FinalizationResult{Usage: record}, nil
}

func (f *LLMRequestFinalizer) MarkPricingFailed(
	ctx context.Context,
	input llmrequest.PricingFailureInput,
) (llmrequest.FinalizationResult, error) {
	if f == nil || f.ledger == nil {
		return llmrequest.FinalizationResult{},
			llmrequest.ErrDependencyRequired
	}
	record, err := f.ledger.MarkPricingFailed(
		ctx,
		ledgerapp.MarkPricingFailedInput{
			LocalRequestID: input.Reserved.Prepared.LocalRequestID,
			Usage: input.Reserved.Prepared.Plan.
				EstimatedUsage,
			UsageCompleteness: pricingapp.
				UsageCompletenessFailed,
			FailureReason: input.FailureReason,
		},
	)
	if err != nil {
		return llmrequest.FinalizationResult{}, fmt.Errorf(
			"mark pricing failed usage: %w",
			err,
		)
	}
	return llmrequest.FinalizationResult{Usage: record}, nil
}
