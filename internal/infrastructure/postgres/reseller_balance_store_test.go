package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestNewResellerBalanceStoreRejectsNilDB(t *testing.T) {
	_, err := NewResellerBalanceStore(nil)
	if !errors.Is(err, ErrInvalidDatabaseConfig) {
		t.Fatalf("error = %v, want invalid database config", err)
	}
}

func TestValidateResellerBalanceReserveInput(t *testing.T) {
	now := time.Unix(100, 0).UTC()

	tests := []struct {
		name       string
		resellerID string
		amount     int64
		at         time.Time
		wantError  bool
	}{
		{
			name:       "valid",
			resellerID: "reseller-1",
			amount:     0,
			at:         now,
		},
		{
			name:       "blank reseller id",
			resellerID: " ",
			amount:     1,
			at:         now,
			wantError:  true,
		},
		{
			name:       "negative amount",
			resellerID: "reseller-1",
			amount:     -1,
			at:         now,
			wantError:  true,
		},
		{
			name:       "zero timestamp",
			resellerID: "reseller-1",
			amount:     1,
			wantError:  true,
		},
		{
			name:       "non UTC timestamp",
			resellerID: "reseller-1",
			amount:     1,
			at: time.Date(
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
			err := validateResellerBalanceReserveInput(
				test.resellerID,
				test.amount,
				test.at,
			)
			if test.wantError {
				if !errors.Is(err, ports.ErrStoreContractViolation) {
					t.Fatalf("error = %v, want contract violation", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("validation error: %v", err)
			}
		})
	}
}
