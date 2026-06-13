package postgres

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestExpectedRecordJSONStrictRoundTrip(t *testing.T) {
	value := testExpectedRecord()

	body, err := encodeExpectedRecord(value)
	if err != nil {
		t.Fatalf("encodeExpectedRecord: %v", err)
	}
	got, err := decodeExpectedRecord(body)
	if err != nil {
		t.Fatalf("decodeExpectedRecord: %v", err)
	}
	if !sameUsageRecord(got, value) {
		t.Fatalf("round trip mismatch:\n got: %+v\nwant: %+v", got, value)
	}
}

func TestExpectedRecordJSONRejectsUnknownField(t *testing.T) {
	body, err := encodeExpectedRecord(testExpectedRecord())
	if err != nil {
		t.Fatal(err)
	}

	var object map[string]json.RawMessage
	if err := json.Unmarshal(body, &object); err != nil {
		t.Fatal(err)
	}
	object["unknown"] = json.RawMessage(`true`)
	body, err = json.Marshal(object)
	if err != nil {
		t.Fatal(err)
	}

	_, err = decodeExpectedRecord(body)
	if !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf("error = %v, want contract violation", err)
	}
}

func TestExpectedRecordJSONRejectsMissingUsageDimension(t *testing.T) {
	body, err := encodeExpectedRecord(testExpectedRecord())
	if err != nil {
		t.Fatal(err)
	}

	body = bytes.Replace(
		body,
		[]byte(`"video_input_tokens":9,`),
		nil,
		1,
	)
	_, err = decodeExpectedRecord(body)
	if !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf("error = %v, want contract violation", err)
	}
}

func TestSameBatchCommandIgnoresLifecycleAndTimestamps(t *testing.T) {
	left := testBatch()
	right := left
	right.Status = domain.BillingChargeStatusSucceeded
	balance := int64(100)
	right.BillingResponseBalanceCents = &balance
	chargedAt := left.CreatedAt.Add(time.Minute)
	right.ChargedAt = &chargedAt
	right.CreatedAt = left.CreatedAt.Add(time.Hour)
	right.UpdatedAt = right.CreatedAt

	if !sameBatchCommand(left, right) {
		t.Fatal("lifecycle and timestamps must not change command identity")
	}
	right.AmountCents++
	if sameBatchCommand(left, right) {
		t.Fatal("amount must change command identity")
	}
}

func TestSameAllocationCommandIgnoresCreatedAt(t *testing.T) {
	left := domain.BillingChargeAllocation{
		ID:                   "allocation-1",
		BatchID:              "batch-1",
		LocalRequestID:       "request-1",
		ChargedAmountCents:   50,
		RemainingAmountCents: 25,
		CreatedAt:            time.Unix(1, 0).UTC(),
	}
	right := left
	right.CreatedAt = time.Unix(2, 0).UTC()
	if !sameAllocationCommand(left, right) {
		t.Fatal("created_at must not change allocation command identity")
	}
}

func TestPostClaimRecordChangesOnlyClaim(t *testing.T) {
	before := testExpectedRecord()
	before.BillingChargeRequestID = "historical"
	after := postClaimRecord(before, "batch-new")

	comparison := before
	comparison.BillingChargeRequestID = "batch-new"
	if !sameUsageRecord(after, comparison) {
		t.Fatalf("post claim record changed non-claim fields")
	}
}

func testBatch() domain.BillingChargeBatch {
	now := time.Unix(100, 200).UTC()
	return domain.BillingChargeBatch{
		ID:                   "batch-1",
		UserID:               "user-1",
		BillingSubjectUserID: "billing-user-1",
		ProviderType:         domain.ProviderOpenAI,
		ClientModel:          "model-1",
		BillingModel:         "openai:model-1",
		InputTokens:          10,
		OutputTokens:         5,
		AmountCents:          50,
		Currency:             "RUB",
		Status:               domain.BillingChargeStatusPending,
		CreatedAt:            now,
		UpdatedAt:            now,
	}
}

func testExpectedRecord() domain.UsageRecord {
	now := time.Unix(100, 200).UTC()
	reservedAt := now.Add(-time.Minute)
	billableAt := now.Add(-time.Second)
	return domain.UsageRecord{
		LocalRequestID:     "request-1",
		IdempotencyKey:     "idem-1",
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
		ProviderRequestID:  "provider-request-1",
		EstimatedUsage: domain.TokenUsage{
			InputTokens:          1,
			CachedInputTokens:    2,
			OutputTokens:         3,
			ReasoningTokens:      4,
			ImageInputTokens:     5,
			AudioInputTokens:     6,
			AudioOutputTokens:    7,
			FileInputTokens:      8,
			VideoInputTokens:     9,
			ImageGenerationUnits: 10,
		},
		Usage: domain.TokenUsage{
			InputTokens:          11,
			CachedInputTokens:    12,
			OutputTokens:         13,
			ReasoningTokens:      14,
			ImageInputTokens:     15,
			AudioInputTokens:     16,
			AudioOutputTokens:    17,
			FileInputTokens:      18,
			VideoInputTokens:     19,
			ImageGenerationUnits: 20,
		},
		EstimatedClientAmountCents: 60,
		EstimatedUpstreamCostCents: 20,
		ClientAmountCents:          50,
		ChargedAmountCents:         0,
		RemainingAmountCents:       50,
		ActualUpstreamCostCents:    15,
		Currency:                   "RUB",
		UsageCompleteness:          "detailed",
		Status:                     domain.UsageStatusBillable,
		BillingChargeRequestID:     "batch-1",
		CreatedAt:                  now.Add(-time.Hour),
		ReservedAt:                 &reservedAt,
		BillableAt:                 &billableAt,
		UpdatedAt:                  billableAt,
	}
}

func TestCanonicalUTCTimeRejectsNonUTC(t *testing.T) {
	if !isCanonicalUTCTime(time.Unix(1, 0).UTC()) {
		t.Fatal("UTC time must be accepted")
	}
	if isCanonicalUTCTime(time.Unix(1, 0).In(
		time.FixedZone("offset", 3*60*60),
	)) {
		t.Fatal("non-UTC time must be rejected")
	}
}
