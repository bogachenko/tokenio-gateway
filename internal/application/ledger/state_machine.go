package ledger

import (
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func CanTransition(from domain.UsageStatus, to domain.UsageStatus) bool {
	if !isKnownStatus(from) || !isKnownStatus(to) {
		return false
	}
	for _, allowed := range allowedTransitions[from] {
		if allowed == to {
			return true
		}
	}
	return false
}

func ValidateTransition(from domain.UsageStatus, to domain.UsageStatus) error {
	if !isKnownStatus(from) {
		return fmt.Errorf("%w: from %s", ErrInvalidUsageStatus, from)
	}
	if !isKnownStatus(to) {
		return fmt.Errorf("%w: to %s", ErrInvalidUsageStatus, to)
	}
	if !CanTransition(from, to) {
		return fmt.Errorf("%w: %s to %s", ErrInvalidStateTransition, from, to)
	}
	return nil
}

func isKnownStatus(status domain.UsageStatus) bool {
	switch status {
	case domain.UsageStatusReserved,
		domain.UsageStatusReleased,
		domain.UsageStatusBillable,
		domain.UsageStatusPartiallyCharged,
		domain.UsageStatusCharged,
		domain.UsageStatusFailed,
		domain.UsageStatusPricingFailed:
		return true
	default:
		return false
	}
}

func isTerminalStatus(status domain.UsageStatus) bool {
	return status == domain.UsageStatusReleased || status == domain.UsageStatusCharged || status == domain.UsageStatusFailed
}

var allowedTransitions = map[domain.UsageStatus][]domain.UsageStatus{
	domain.UsageStatusReserved: {
		domain.UsageStatusReleased,
		domain.UsageStatusBillable,
		domain.UsageStatusFailed,
		domain.UsageStatusPricingFailed,
	},
	domain.UsageStatusBillable: {
		domain.UsageStatusCharged,
		domain.UsageStatusPartiallyCharged,
		domain.UsageStatusFailed,
	},
	domain.UsageStatusPartiallyCharged: {
		domain.UsageStatusCharged,
		domain.UsageStatusPartiallyCharged,
		domain.UsageStatusFailed,
	},
	domain.UsageStatusPricingFailed: {
		domain.UsageStatusBillable,
		domain.UsageStatusCharged,
		domain.UsageStatusFailed,
	},
}
