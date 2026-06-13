package postgres

import (
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/jackc/pgx/v5"
)

func TestUsageRecordNamedArgsContainsAllUsageDimensions(t *testing.T) {
	now := time.Unix(100, 200).UTC()
	record := domain.UsageRecord{
		LocalRequestID: "request-1",
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
		CreatedAt: now,
		UpdatedAt: now,
	}

	args := usageRecordNamedArgs(record)
	expected := map[string]int64{
		"estimated_input_tokens":           1,
		"estimated_cached_input_tokens":    2,
		"estimated_output_tokens":          3,
		"estimated_reasoning_tokens":       4,
		"estimated_image_input_tokens":     5,
		"estimated_audio_input_tokens":     6,
		"estimated_audio_output_tokens":    7,
		"estimated_file_input_tokens":      8,
		"estimated_video_input_tokens":     9,
		"estimated_image_generation_units": 10,
		"input_tokens":                     11,
		"cached_input_tokens":              12,
		"output_tokens":                    13,
		"reasoning_tokens":                 14,
		"image_input_tokens":               15,
		"audio_input_tokens":               16,
		"audio_output_tokens":              17,
		"file_input_tokens":                18,
		"video_input_tokens":               19,
		"image_generation_units":           20,
	}
	for key, want := range expected {
		got, ok := args[key].(int64)
		if !ok || got != want {
			t.Errorf("args[%q] = %#v, want %d", key, args[key], want)
		}
	}
}

func TestUsageRecordNamedArgsStoresEmptyOptionalStringsAsNull(t *testing.T) {
	args := usageRecordNamedArgs(domain.UsageRecord{})
	for _, key := range []string{
		"idempotency_key",
		"api_key_id",
		"selected_reseller_id",
		"selected_route_id",
		"provider_request_id",
		"provider_response_model",
		"failure_reason",
		"billing_charge_request_id",
	} {
		if args[key] != nil {
			t.Errorf("args[%q] = %#v, want nil", key, args[key])
		}
	}
}

func TestUsageLedgerSQLContainsRequiredConcurrencyBoundaries(t *testing.T) {
	checks := map[string][]string{
		"insert": {
			"INSERT INTO tokenio_usage_records",
			"@estimated_image_generation_units",
			"@image_generation_units",
		},
		"cas": {
			"WHERE local_request_id = @lookup_local_request_id",
			"AND status = @expected_status",
		},
		"candidates": {
			"usage.status = 'billable'",
			"usage.status = 'partially_charged'",
			"historical_batch.billing_status = 'succeeded'",
			"active_batch.billing_status IN ('pending', 'failed')",
			"ORDER BY usage.created_at ASC, usage.local_request_id ASC",
		},
	}
	sqlByName := map[string]string{
		"insert":     insertUsageRecordSQL,
		"cas":        updateUsageRecordCASQL,
		"candidates": loadChargeCandidatesSQL,
	}
	for name, fragments := range checks {
		for _, fragment := range fragments {
			if !strings.Contains(sqlByName[name], fragment) {
				t.Errorf("%s SQL missing %q", name, fragment)
			}
		}
	}
}

func TestNewUsageLedgerRejectsNilDB(t *testing.T) {
	_, err := NewUsageLedger(nil)
	if err != ErrInvalidDatabaseConfig {
		t.Fatalf("error = %v, want ErrInvalidDatabaseConfig", err)
	}
}

func TestUsageRecordNamedArgsIsPGXNamedArgs(t *testing.T) {
	var _ pgx.NamedArgs = usageRecordNamedArgs(domain.UsageRecord{})
}
