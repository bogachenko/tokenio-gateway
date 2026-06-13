package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"math"
	"net"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

func normalizeRegistryReadError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) ||
		errors.Is(err, context.DeadlineExceeded) ||
		errors.Is(err, pgx.ErrNoRows) ||
		errors.Is(err, ports.ErrNotFound) ||
		errors.Is(err, ports.ErrStoreUnavailable) ||
		errors.Is(err, ports.ErrStoreConflict) ||
		errors.Is(err, ports.ErrStoreContractViolation) ||
		errors.Is(err, ErrInvalidDatabaseConfig) {
		return NormalizeError(err)
	}

	var pgError *pgconn.PgError
	if errors.As(err, &pgError) {
		return NormalizeError(err)
	}
	var networkError net.Error
	if errors.As(err, &networkError) ||
		pgconn.Timeout(err) ||
		pgconn.SafeToRetry(err) {
		return ports.ErrStoreUnavailable
	}
	return ports.ErrStoreContractViolation
}

func canonicalTime(value time.Time) time.Time {
	return value.UTC()
}

func optionalText(value pgtype.Text) string {
	if !value.Valid {
		return ""
	}
	return value.String
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	canonical := canonicalTime(value.Time)
	return &canonical
}

func uniqueIDs(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}

	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}

func decodeCapabilities(raw []byte) (domain.CapabilitySet, error) {
	var result domain.CapabilitySet
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) < 2 || trimmed[0] != '{' || trimmed[len(trimmed)-1] != '}' {
		return result, ports.ErrStoreContractViolation
	}

	decoder := json.NewDecoder(bytes.NewReader(trimmed))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return domain.CapabilitySet{}, ports.ErrStoreContractViolation
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return domain.CapabilitySet{}, ports.ErrStoreContractViolation
	}
	return result, nil
}

func validateRoutePricePersistence(value domain.RoutePrice) error {
	if value.MarkupCoefficient <= 0 ||
		math.IsNaN(value.MarkupCoefficient) ||
		math.IsInf(value.MarkupCoefficient, 0) {
		return ports.ErrStoreContractViolation
	}
	return nil
}
