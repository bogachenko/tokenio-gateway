package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestValidateUsageResellerBillableInput(t *testing.T) {
	now := time.Unix(100, 0).UTC()
	reservedAt := now
	billableAt := now.Add(time.Second)
	valid := domain.UsageRecord{
		LocalRequestID:          "request-1",
		Status:                  domain.UsageStatusBillable,
		Currency:                "RUB",
		UsageCompleteness:       "detailed",
		ClientAmountCents:       10,
		RemainingAmountCents:    10,
		ActualUpstreamCostCents: 4,
		CreatedAt:               now,
		ReservedAt:              &reservedAt,
		BillableAt:              &billableAt,
		UpdatedAt:               billableAt,
	}

	tests := []struct {
		name      string
		mutate    func(domain.UsageRecord) domain.UsageRecord
		requestID string
		wantError bool
	}{
		{
			name:      "valid",
			mutate:    identityUsageRecord,
			requestID: "request-1",
		},
		{
			name:      "blank request id",
			mutate:    identityUsageRecord,
			requestID: " ",
			wantError: true,
		},
		{
			name: "wrong status",
			mutate: func(value domain.UsageRecord) domain.UsageRecord {
				value.Status = domain.UsageStatusReserved
				return value
			},
			requestID: "request-1",
			wantError: true,
		},
		{
			name: "remaining amount mismatch",
			mutate: func(value domain.UsageRecord) domain.UsageRecord {
				value.RemainingAmountCents++
				return value
			},
			requestID: "request-1",
			wantError: true,
		},
		{
			name: "negative usage",
			mutate: func(value domain.UsageRecord) domain.UsageRecord {
				value.Usage.InputTokens = -1
				return value
			},
			requestID: "request-1",
			wantError: true,
		},
		{
			name: "non UTC timestamp",
			mutate: func(value domain.UsageRecord) domain.UsageRecord {
				updatedAt := value.UpdatedAt.In(
					time.FixedZone("offset", 3600),
				)
				value.UpdatedAt = updatedAt
				value.BillableAt = &updatedAt
				return value
			},
			requestID: "request-1",
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateUsageResellerBillableInput(
				test.requestID,
				test.mutate(valid),
			)
			if test.wantError {
				if !errors.Is(
					err,
					ports.ErrStoreContractViolation,
				) {
					t.Fatalf(
						"error = %v, want contract violation",
						err,
					)
				}
				return
			}
			if err != nil {
				t.Fatalf("validation error: %v", err)
			}
		})
	}
}

func identityUsageRecord(
	value domain.UsageRecord,
) domain.UsageRecord {
	return value
}
