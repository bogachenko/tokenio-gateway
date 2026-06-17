package billing

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const currencyRUB = "RUB"

type AdmissionConfig struct {
	MinimumRequestBalanceCents int64
}

type AdmissionService struct {
	identity ports.BillingIdentityService
	balance  ports.BillingBalanceClient
	ledger   ports.UsageLedger
	config   AdmissionConfig
}

type AdmissionInput struct {
	UserID               string
	BillingSubjectUserID string
	RequiredReserveCents int64
	Currency             string
}

type AdmissionResult struct {
	Allowed               bool
	RemoteBalanceCents    int64
	PendingAmountCents    int64
	EffectiveBalanceCents int64
	RequiredReserveCents  int64
	Currency              string
}

func NewAdmissionService(
	identity ports.BillingIdentityService,
	balance ports.BillingBalanceClient,
	usageLedger ports.UsageLedger,
	config AdmissionConfig,
) (*AdmissionService, error) {
	if identity == nil || balance == nil || usageLedger == nil {
		return nil, fmt.Errorf("%w: dependency", ErrInvalidBillingInput)
	}
	if config.MinimumRequestBalanceCents < 0 {
		return nil, fmt.Errorf("%w: minimum request balance", ErrInvalidBillingInput)
	}
	return &AdmissionService{
		identity: identity,
		balance:  balance,
		ledger:   usageLedger,
		config:   config,
	}, nil
}

func (s *AdmissionService) Admit(ctx context.Context, input AdmissionInput) (AdmissionResult, error) {
	var result AdmissionResult
	if strings.TrimSpace(input.UserID) == "" || strings.TrimSpace(input.BillingSubjectUserID) == "" || input.RequiredReserveCents < 0 {
		return result, fmt.Errorf("%w: admission", ErrInvalidBillingInput)
	}
	currency := input.Currency
	if currency == "" {
		currency = currencyRUB
	}
	if currency != currencyRUB {
		return result, fmt.Errorf("%w: currency", ErrInvalidBillingInput)
	}
	token, err := s.identity.TokenForSubject(ctx, input.BillingSubjectUserID)
	if err != nil {
		return result, ErrBillingIdentityUnavailable
	}
	remote, err := s.balance.GetBalance(ctx, token)
	if err != nil {
		return result, ErrBillingUnavailable
	}
	if err := validateBillingBalance(remote); err != nil {
		return result, err
	}
	snapshot, err := s.ledger.LoadExposure(ctx, input.UserID, currency)
	if err != nil {
		return result, ErrBillingStoreUnavailable
	}
	exposure, err := domain.CalculateUsageExposure(
		snapshot.Currency,
		snapshot.ReservedEstimatedAmountCents,
		snapshot.BillableRemainingAmountCents,
		snapshot.PartiallyChargedRemainingAmountCents,
		snapshot.PricingFailedCount,
	)
	if err != nil {
		if errors.Is(err, domain.ErrUnresolvedUsage) {
			return result, ErrUnresolvedUsage
		}
		return result, ErrBillingStoreUnavailable
	}
	requiredBalanceCents := input.RequiredReserveCents
	if s.config.MinimumRequestBalanceCents > requiredBalanceCents {
		requiredBalanceCents = s.config.MinimumRequestBalanceCents
	}
	balanceResult, err := domain.EvaluateBalance(domain.BalanceInput{
		RemoteBalanceCents:   remote.BalanceCents,
		RequiredReserveCents: requiredBalanceCents,
		Exposure:             exposure,
	})
	result = AdmissionResult{
		Allowed:               balanceResult.Allowed,
		RemoteBalanceCents:    balanceResult.RemoteBalanceCents,
		PendingAmountCents:    balanceResult.PendingAmountCents,
		EffectiveBalanceCents: balanceResult.EffectiveBalanceCents,
		RequiredReserveCents:  input.RequiredReserveCents,
		Currency:              currency,
	}
	if errors.Is(err, domain.ErrUnresolvedUsage) {
		return result, ErrUnresolvedUsage
	}
	if err != nil {
		return result, err
	}
	return result, nil
}

func validateBillingBalance(balance ports.BillingBalance) error {
	if balance.Currency != currencyRUB || balance.BalanceCents < 0 {
		return ErrBillingUnavailable
	}
	return nil
}
