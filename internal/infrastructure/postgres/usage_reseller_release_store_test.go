package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestValidateUsageResellerReleaseInput(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	tests := []struct {
		name           string
		localRequestID string
		failureReason  string
		releasedAt     time.Time
		wantError      bool
	}{
		{
			name:           "valid",
			localRequestID: "request-1",
			failureReason:  "connection_error",
			releasedAt:     now,
		},
		{
			name:           "blank local request id",
			localRequestID: " ",
			failureReason:  "connection_error",
			releasedAt:     now,
			wantError:      true,
		},
		{
			name:           "blank failure reason",
			localRequestID: "request-1",
			failureReason:  " ",
			releasedAt:     now,
			wantError:      true,
		},
		{
			name:           "zero release timestamp",
			localRequestID: "request-1",
			failureReason:  "connection_error",
			wantError:      true,
		},
		{
			name:           "non UTC release timestamp",
			localRequestID: "request-1",
			failureReason:  "connection_error",
			releasedAt: time.Date(
				2026,
				time.June,
				14,
				12,
				0,
				0,
				0,
				time.FixedZone("offset", 3600),
			),
			wantError: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateUsageResellerReleaseInput(
				test.localRequestID,
				test.failureReason,
				test.releasedAt,
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
