package postgres

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestRecordLastUsedAtRejectsInvalidContractBeforeDB(
	t *testing.T,
) {
	now := time.Date(
		2026,
		time.June,
		13,
		12,
		0,
		0,
		0,
		time.UTC,
	)

	tests := []struct {
		name     string
		apiKeyID string
		usedAt   time.Time
	}{
		{
			name:   "empty id",
			usedAt: now,
		},
		{
			name:     "id with surrounding whitespace",
			apiKeyID: " key_1 ",
			usedAt:   now,
		},
		{
			name:     "zero timestamp",
			apiKeyID: "key_1",
		},
		{
			name:     "non UTC timestamp",
			apiKeyID: "key_1",
			usedAt: time.Date(
				2026,
				time.June,
				13,
				12,
				0,
				0,
				0,
				time.FixedZone("UTC+1", 3600),
			),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &APIKeyRepository{}
			err := repository.RecordLastUsedAt(
				t.Context(),
				test.apiKeyID,
				test.usedAt,
			)
			if !errors.Is(
				err,
				ports.ErrStoreContractViolation,
			) {
				t.Fatalf(
					"error=%v want store contract violation",
					err,
				)
			}
		})
	}
}

func TestRecordLastUsedAtRejectsMissingDatabase(
	t *testing.T,
) {
	repository := &APIKeyRepository{}
	err := repository.RecordLastUsedAt(
		t.Context(),
		"key_1",
		time.Date(
			2026,
			time.June,
			13,
			12,
			0,
			0,
			0,
			time.UTC,
		),
	)
	if !errors.Is(
		err,
		ErrInvalidDatabaseConfig,
	) {
		t.Fatalf(
			"error=%v want invalid database config",
			err,
		)
	}
}

func TestRecordLastUsedAtSQLIsMonotonicAndDoesNotUpsert(
	t *testing.T,
) {
	lower := strings.ToLower(
		recordAPIKeyLastUsedAtSQL,
	)

	for _, required := range []string{
		"update tokenio_api_keys",
		"last_used_at = case",
		"last_used_at is null or last_used_at < $2",
		"enabled = true",
		"revoked_at is null",
		"expires_at is null or expires_at > $2",
		"returning last_used_at",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf(
				"usage SQL is missing %q: %s",
				required,
				recordAPIKeyLastUsedAtSQL,
			)
		}
	}

	for _, forbidden := range []string{
		"insert into",
		"on conflict",
		"key_hash",
		"key_prefix",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf(
				"usage SQL contains forbidden %q: %s",
				forbidden,
				recordAPIKeyLastUsedAtSQL,
			)
		}
	}
}
