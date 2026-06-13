package ledger

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestStage10V7CanonicalBillingModelDoesNotMaskExistingRecordDiagnostics(t *testing.T) {
	at := time.Unix(10, 0).UTC()
	record := domain.UsageRecord{
		LocalRequestID: "llmreq_order",
		Status:         domain.UsageStatusReserved,
		CreatedAt:      at,
		UpdatedAt:      at,
	}
	err := ValidateRecord(record)
	if !errors.Is(err, ErrRecordCorrupt) || !strings.Contains(err.Error(), "reserved timestamps") {
		t.Fatalf("error = %v, want reserved timestamp diagnostic before billing model", err)
	}
}

func TestStage10V7CanonicalBillingModelIsStillRequiredForOtherwiseValidRecord(t *testing.T) {
	at := time.Unix(20, 0).UTC()
	reservedAt := at
	record := domain.UsageRecord{
		LocalRequestID:    "llmreq_model_required",
		Status:            domain.UsageStatusReserved,
		UsageCompleteness: "missing",
		CreatedAt:         at,
		ReservedAt:        &reservedAt,
		UpdatedAt:         at,
	}
	err := ValidateRecord(record)
	if !errors.Is(err, ErrRecordCorrupt) || !strings.Contains(err.Error(), "billing model") {
		t.Fatalf("error = %v, want canonical billing model rejection", err)
	}

	record.ProviderType = domain.ProviderOpenAI
	record.ClientModel = "model-a"
	record.BillingModel = "openai:model-a"
	if err := ValidateRecord(record); err != nil {
		t.Fatalf("canonical record rejected: %v", err)
	}
}
