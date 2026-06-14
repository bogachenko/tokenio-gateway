package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const recordAPIKeyLastUsedAtSQL = `
UPDATE tokenio_api_keys
SET
    last_used_at = CASE
        WHEN last_used_at IS NULL OR last_used_at < $2
            THEN $2
        ELSE last_used_at
    END,
    updated_at = CASE
        WHEN last_used_at IS NULL OR last_used_at < $2
            THEN GREATEST(updated_at, $2)
        ELSE updated_at
    END
WHERE id = $1
  AND enabled = TRUE
  AND revoked_at IS NULL
  AND (expires_at IS NULL OR expires_at > $2)
RETURNING last_used_at
`

var _ ports.APIKeyUsageRecorder = (*APIKeyRepository)(nil)

func (r *APIKeyRepository) RecordLastUsedAt(
	ctx context.Context,
	apiKeyID string,
	usedAt time.Time,
) error {
	if ctx == nil ||
		apiKeyID == "" ||
		apiKeyID != strings.TrimSpace(apiKeyID) ||
		usedAt.IsZero() ||
		usedAt.Location() != time.UTC {
		return ports.ErrStoreContractViolation
	}
	if r == nil || r.db == nil {
		return ErrInvalidDatabaseConfig
	}

	var persisted time.Time
	if err := r.db.QueryRow(
		ctx,
		recordAPIKeyLastUsedAtSQL,
		apiKeyID,
		usedAt,
	).Scan(&persisted); err != nil {
		return normalizeRegistryReadError(err)
	}

	persisted = canonicalTime(persisted)
	if persisted.Before(usedAt) {
		return ports.ErrStoreContractViolation
	}
	return nil
}
