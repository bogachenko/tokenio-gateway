package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestNewAdminUsageLedgerRejectsNilDB(t *testing.T) {
	_, err := NewAdminUsageLedger(nil)
	if !errors.Is(err, ErrInvalidDatabaseConfig) {
		t.Fatalf("error = %v, want invalid database config", err)
	}
}

func TestValidateAdminUsageResolutionShapes(t *testing.T) {
	base := adminPricingFailedRecord()

	tests := []struct {
		name   string
		action domain.AuditAction
		next   func(domain.UsageRecord) domain.UsageRecord
	}{
		{
			name:   "billable",
			action: domain.AuditActionUsageResolveBillable,
			next: func(value domain.UsageRecord) domain.UsageRecord {
				at := value.UpdatedAt.Add(time.Second)
				value.Status = domain.UsageStatusBillable
				value.Usage = domain.TokenUsage{
					InputTokens:  10,
					OutputTokens: 5,
				}
				value.UsageCompleteness = "estimated"
				value.ClientAmountCents = 100
				value.ChargedAmountCents = 0
				value.RemainingAmountCents = 100
				value.ActualUpstreamCostCents = 40
				value.FailureReason = ""
				value.BillableAt = &at
				value.FailedAt = nil
				value.UpdatedAt = at
				return value
			},
		},
		{
			name:   "failed",
			action: domain.AuditActionUsageResolveFailed,
			next: func(value domain.UsageRecord) domain.UsageRecord {
				at := value.UpdatedAt.Add(time.Second)
				value.Status = domain.UsageStatusFailed
				value.ClientAmountCents = 0
				value.ChargedAmountCents = 0
				value.RemainingAmountCents = 0
				value.FailureReason = "manual_resolution"
				value.FailedAt = &at
				value.UpdatedAt = at
				return value
			},
		},
		{
			name:   "charged",
			action: domain.AuditActionUsageResolveCharged,
			next: func(value domain.UsageRecord) domain.UsageRecord {
				at := value.UpdatedAt.Add(time.Second)
				value.Status = domain.UsageStatusCharged
				value.UsageCompleteness = "estimated"
				value.ClientAmountCents = 100
				value.ChargedAmountCents = 100
				value.RemainingAmountCents = 0
				value.FailureReason = ""
				value.BillingChargeRequestID = "billchg_manual"
				value.BillableAt = &at
				value.ChargedAt = &at
				value.FailedAt = nil
				value.UpdatedAt = at
				return value
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			next := test.next(base)
			if err := validateAdminUsageResolution(
				base,
				next,
				test.action,
			); err != nil {
				t.Fatalf("validation error: %v", err)
			}
		})
	}
}

func TestValidateAdminUsageResolutionRejectsImmutableChange(
	t *testing.T,
) {
	base := adminPricingFailedRecord()
	next := base
	next.Status = domain.UsageStatusFailed
	next.UserID = "different"
	at := next.UpdatedAt.Add(time.Second)
	next.FailedAt = &at
	next.UpdatedAt = at

	if err := validateAdminUsageResolution(
		base,
		next,
		domain.AuditActionUsageResolveFailed,
	); !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf("error = %v, want contract violation", err)
	}
}

func TestSameAdminChargeSnapshotIncludesLifecycle(t *testing.T) {
	left := adminFailedChargeSnapshot()
	right := left
	if !sameAdminChargeSnapshot(left, right) {
		t.Fatal("identical snapshots differ")
	}
	right.Batch.BillingErrorCode = "different"
	if sameAdminChargeSnapshot(left, right) {
		t.Fatal("batch lifecycle difference was ignored")
	}
}

func TestBillingRetryAuditStateMatchesApplicationShape(
	t *testing.T,
) {
	snapshot := adminFailedChargeSnapshot()
	state := adminBillingBatchApplicationState(snapshot.Batch)
	if state["billing_status"] !=
		domain.BillingChargeStatusFailed {
		t.Fatalf("state = %+v", state)
	}
	if auditStateContainsSecret(state) {
		t.Fatal("billing batch audit state contains secret")
	}
}

func adminPricingFailedRecord() domain.UsageRecord {
	now := time.Unix(100, 123456000).UTC()
	failedAt := now
	return domain.UsageRecord{
		LocalRequestID:     "request-pricing-failed",
		UserID:             "user-1",
		APIKeyID:           "key-1",
		APIFamily:          domain.APIFamilyOpenAICompatible,
		EndpointKind:       domain.EndpointChat,
		ClientModel:        "model-1",
		BillingModel:       "openai:model-1",
		SelectedRouteID:    "route-1",
		SelectedResellerID: "reseller-1",
		ProviderType:       domain.ProviderOpenAI,
		ProviderModel:      "model-1",
		EstimatedUsage: domain.TokenUsage{
			InputTokens:  10,
			OutputTokens: 5,
		},
		EstimatedClientAmountCents: 100,
		EstimatedUpstreamCostCents: 40,
		Currency:                   "RUB",
		UsageCompleteness:          "failed",
		Status:                     domain.UsageStatusPricingFailed,
		FailureReason:              "pricing_unavailable",
		CreatedAt:                  now.Add(-time.Minute),
		ReservedAt:                 timePointerForAdminUsage(now.Add(-time.Minute)),
		FailedAt:                   &failedAt,
		UpdatedAt:                  now,
	}
}

func adminFailedChargeSnapshot() ports.BillingChargeBatchSnapshot {
	now := time.Unix(100, 123456000).UTC()
	failedAt := now.Add(time.Second)
	batch := domain.BillingChargeBatch{
		ID:                   "billchg_1",
		UserID:               "user-1",
		BillingSubjectUserID: "billing-user-1",
		ProviderType:         domain.ProviderOpenAI,
		ClientModel:          "model-1",
		BillingModel:         "openai:model-1",
		InputTokens:          10,
		OutputTokens:         5,
		AmountCents:          100,
		Currency:             "RUB",
		Status:               domain.BillingChargeStatusFailed,
		BillingErrorCode:     "billing_unavailable",
		CreatedAt:            now,
		FailedAt:             &failedAt,
		UpdatedAt:            failedAt,
	}
	record := adminPricingFailedRecord()
	record.Status = domain.UsageStatusBillable
	record.UsageCompleteness = "estimated"
	record.ClientAmountCents = 100
	record.RemainingAmountCents = 100
	record.FailureReason = ""
	record.FailedAt = nil
	record.BillableAt = &now
	record.BillingChargeRequestID = batch.ID
	return ports.BillingChargeBatchSnapshot{
		Batch: batch,
		Allocations: []domain.BillingChargeAllocation{
			{
				ID:                   "allocation-1",
				BatchID:              batch.ID,
				LocalRequestID:       record.LocalRequestID,
				ChargedAmountCents:   100,
				RemainingAmountCents: 0,
				CreatedAt:            now,
			},
		},
		ExpectedRecords: []domain.UsageRecord{record},
	}
}

func timePointerForAdminUsage(value time.Time) *time.Time {
	return &value
}

func TestCanonicalBillingRetryAuditInputTruncatesTimestamps(
	t *testing.T,
) {
	value := time.Unix(100, 123456789).UTC()
	audit := domain.AuditContext{
		ID:           "audit-1",
		AdminSubject: "admin_token",
		Action:       domain.AuditActionBillingChargeRetry,
		EntityType:   "billing_charge_batch",
		EntityID:     "billchg-1",
		BeforeState: domain.AuditState{
			"created_at": value,
			"failed_at":  &value,
		},
		AfterState: domain.AuditState{
			"updated_at": value,
			"charged_at": &value,
		},
		RequestID: "admreq-1",
		CreatedAt: value,
	}
	got := canonicalBillingRetryAuditInput(audit)
	want := postgresAdminTime(value)

	if !got.CreatedAt.Equal(want) {
		t.Fatalf("created_at = %s, want %s", got.CreatedAt, want)
	}
	for _, state := range []domain.AuditState{
		got.BeforeState,
		got.AfterState,
	} {
		for _, key := range []string{
			"created_at",
			"updated_at",
		} {
			if typed, ok := state[key].(time.Time); ok &&
				!typed.Equal(want) {
				t.Fatalf("%s = %s, want %s", key, typed, want)
			}
		}
		for _, key := range []string{
			"failed_at",
			"charged_at",
		} {
			if typed, ok := state[key].(*time.Time); ok &&
				typed != nil &&
				!typed.Equal(want) {
				t.Fatalf("%s = %s, want %s", key, typed, want)
			}
		}
	}
}

func TestSameAdminBillingBatchUsesPostgresTimePrecision(
	t *testing.T,
) {
	left := adminFailedChargeSnapshot().Batch
	right := left
	value := time.Unix(100, 123456789).UTC()
	left.FailedAt = &value
	truncated := postgresAdminTime(value)
	right.FailedAt = &truncated

	if !sameAdminBillingBatch(left, right) {
		t.Fatal("PostgreSQL-equivalent timestamps differ")
	}
}

func TestValidateAdminUsageResolutionRejectsMissingCompleteness(
	t *testing.T,
) {
	base := adminPricingFailedRecord()
	next := base
	at := next.UpdatedAt.Add(time.Second)
	next.Status = domain.UsageStatusBillable
	next.UsageCompleteness = "missing"
	next.ClientAmountCents = 100
	next.RemainingAmountCents = 100
	next.FailureReason = ""
	next.BillableAt = &at
	next.FailedAt = nil
	next.UpdatedAt = at

	if err := validateAdminUsageResolution(
		base,
		next,
		domain.AuditActionUsageResolveBillable,
	); !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf("error = %v, want contract violation", err)
	}
}
