package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const adminResellerColumns = `
    id,
    name,
    provider_type,
    base_url,
    api_key_env,
    enabled,
    balance_cents,
    reserved_cents,
    minimum_balance_cents,
    created_at,
    updated_at,
    disabled_at
`

const findAdminResellerByIDSQL = `
SELECT
` + adminResellerColumns + `
FROM tokenio_resellers
WHERE id = $1
`

const findAdminResellerByIDForUpdateSQL = `
SELECT
` + adminResellerColumns + `
FROM tokenio_resellers
WHERE id = $1
FOR UPDATE
`

const insertAdminResellerSQL = `
INSERT INTO tokenio_resellers (
    id,
    name,
    provider_type,
    base_url,
    api_key_env,
    enabled,
    balance_cents,
    reserved_cents,
    minimum_balance_cents,
    created_at,
    updated_at,
    disabled_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
RETURNING
` + adminResellerColumns

const updateAdminResellerSQL = `
UPDATE tokenio_resellers
SET
    name = $2,
    provider_type = $3,
    base_url = $4,
    api_key_env = $5,
    enabled = $6,
    balance_cents = $7,
    reserved_cents = $8,
    minimum_balance_cents = $9,
    created_at = $10,
    updated_at = $11,
    disabled_at = $12
WHERE id = $1
RETURNING
` + adminResellerColumns

type AdminResellerRepository struct {
	db *DB
}

var _ ports.ResellerRepository = (*AdminResellerRepository)(nil)

func NewAdminResellerRepository(
	db *DB,
) (*AdminResellerRepository, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &AdminResellerRepository{db: db}, nil
}

func (r *AdminResellerRepository) FindResellerByID(
	ctx context.Context,
	resellerID string,
) (*domain.Reseller, error) {
	value, err := scanReseller(
		r.db.QueryRow(ctx, findAdminResellerByIDSQL, resellerID),
	)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func (r *AdminResellerRepository) ListResellers(
	ctx context.Context,
	filter ports.ResellerListFilter,
) (ports.Page[domain.Reseller], error) {
	if err := validateAdminPage(filter.Page); err != nil {
		return ports.Page[domain.Reseller]{}, err
	}

	where, args := buildAdminResellerFilter(filter)
	var result ports.Page[domain.Reseller]

	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			countSQL := "SELECT COUNT(*) FROM tokenio_resellers" + where
			if err := tx.QueryRow(ctx, countSQL, args...).Scan(&result.Total); err != nil {
				return normalizeRegistryReadError(err)
			}

			listArgs := append([]any(nil), args...)
			limitPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Limit)
			offsetPosition := len(listArgs) + 1
			listArgs = append(listArgs, filter.Page.Offset)

			listSQL := `
SELECT
` + adminResellerColumns + `
FROM tokenio_resellers` + where + fmt.Sprintf(`
ORDER BY created_at DESC, id ASC
LIMIT $%d OFFSET $%d
`, limitPosition, offsetPosition)

			rows, err := tx.Query(ctx, listSQL, listArgs...)
			if err != nil {
				return normalizeRegistryReadError(err)
			}
			defer rows.Close()

			result.Items = make([]domain.Reseller, 0)
			for rows.Next() {
				value, err := scanReseller(rows)
				if err != nil {
					return err
				}
				result.Items = append(result.Items, value)
			}
			if err := rows.Err(); err != nil {
				return normalizeRegistryReadError(err)
			}
			return nil
		},
	)
	if err != nil {
		return ports.Page[domain.Reseller]{}, err
	}
	return result, nil
}

func (r *AdminResellerRepository) CreateResellerWithAudit(
	ctx context.Context,
	requested domain.Reseller,
	audit domain.AuditContext,
) (domain.Reseller, error) {
	if err := validateAdminResellerRecord(requested); err != nil {
		return domain.Reseller{}, err
	}
	if requested.ReservedCents != 0 ||
		requested.DisabledAt != nil ||
		!postgresAdminTime(requested.CreatedAt).Equal(
			postgresAdminTime(requested.UpdatedAt),
		) {
		return domain.Reseller{}, ports.ErrStoreContractViolation
	}
	if err := validateAuditForEntity(
		audit,
		domain.AuditActionResellerCreate,
		"reseller",
		requested.ID,
		domain.AuditState{},
		adminResellerApplicationState(requested),
		requested.CreatedAt,
	); err != nil {
		return domain.Reseller{}, err
	}

	persistedInput := canonicalAdminReseller(requested)
	var created domain.Reseller
	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			value, err := scanReseller(tx.QueryRow(
				ctx,
				insertAdminResellerSQL,
				persistedInput.ID,
				persistedInput.Name,
				string(persistedInput.ProviderType),
				persistedInput.BaseURL,
				persistedInput.APIKeyEnv,
				persistedInput.Enabled,
				persistedInput.BalanceCents,
				persistedInput.ReservedCents,
				persistedInput.MinimumBalanceCents,
				persistedInput.CreatedAt,
				persistedInput.UpdatedAt,
				adminResellerTimeArg(persistedInput.DisabledAt),
			))
			if err != nil {
				return normalizeAdminWriteError(err)
			}
			if !sameAdminReseller(value, persistedInput) {
				return ports.ErrStoreContractViolation
			}

			persistedAudit := canonicalResellerAudit(
				audit,
				domain.AuditState{},
				adminResellerState(value),
				value.CreatedAt,
			)
			if err := insertAdminAudit(ctx, tx, persistedAudit); err != nil {
				return err
			}
			created = value
			return nil
		},
	)
	if err != nil {
		if errors.Is(err, ports.ErrStoreConflict) {
			return domain.Reseller{}, ports.ErrAdminConflict
		}
		return domain.Reseller{}, err
	}
	return created, nil
}

func (r *AdminResellerRepository) CompareAndSwapResellerWithAudit(
	ctx context.Context,
	expected domain.Reseller,
	next domain.Reseller,
	audit domain.AuditContext,
) (domain.Reseller, error) {
	if err := validateAdminResellerRecord(expected); err != nil {
		return domain.Reseller{}, err
	}
	if err := validateAdminResellerRecord(next); err != nil {
		return domain.Reseller{}, err
	}
	if err := validateAdminResellerMutation(expected, next, audit.Action); err != nil {
		return domain.Reseller{}, err
	}
	if err := validateAuditForEntity(
		audit,
		audit.Action,
		"reseller",
		next.ID,
		adminResellerApplicationState(expected),
		adminResellerApplicationState(next),
		next.UpdatedAt,
	); err != nil {
		return domain.Reseller{}, err
	}

	persistedNext := canonicalAdminReseller(next)
	var updated domain.Reseller
	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			current, err := scanReseller(tx.QueryRow(
				ctx,
				findAdminResellerByIDForUpdateSQL,
				expected.ID,
			))
			if err != nil {
				return err
			}
			if !sameAdminReseller(current, expected) {
				return ports.ErrAdminStateConflict
			}

			value, err := scanReseller(tx.QueryRow(
				ctx,
				updateAdminResellerSQL,
				persistedNext.ID,
				persistedNext.Name,
				string(persistedNext.ProviderType),
				persistedNext.BaseURL,
				persistedNext.APIKeyEnv,
				persistedNext.Enabled,
				persistedNext.BalanceCents,
				persistedNext.ReservedCents,
				persistedNext.MinimumBalanceCents,
				persistedNext.CreatedAt,
				persistedNext.UpdatedAt,
				adminResellerTimeArg(persistedNext.DisabledAt),
			))
			if err != nil {
				return normalizeAdminWriteError(err)
			}
			if !sameAdminReseller(value, persistedNext) {
				return ports.ErrStoreContractViolation
			}

			persistedAudit := canonicalResellerAudit(
				audit,
				adminResellerState(current),
				adminResellerState(value),
				value.UpdatedAt,
			)
			if err := insertAdminAudit(ctx, tx, persistedAudit); err != nil {
				return err
			}
			updated = value
			return nil
		},
	)
	if err != nil {
		if errors.Is(err, ports.ErrStoreConflict) {
			return domain.Reseller{}, ports.ErrAdminStateConflict
		}
		return domain.Reseller{}, err
	}
	return updated, nil
}

func validateAdminResellerRecord(value domain.Reseller) error {
	if value.ID == "" ||
		value.Name == "" ||
		value.ProviderType == "" ||
		value.BaseURL == "" ||
		value.APIKeyEnv == "" ||
		value.ReservedCents < 0 ||
		value.MinimumBalanceCents < 0 ||
		!isAdminUTCTime(value.CreatedAt) ||
		!isAdminUTCTime(value.UpdatedAt) ||
		postgresAdminTime(value.UpdatedAt).Before(
			postgresAdminTime(value.CreatedAt),
		) ||
		value.DisabledAt != nil &&
			!isAdminUTCTime(*value.DisabledAt) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validateAdminResellerMutation(
	expected domain.Reseller,
	next domain.Reseller,
	action domain.AuditAction,
) error {
	if expected.ID != next.ID ||
		expected.ProviderType != next.ProviderType ||
		!postgresAdminTime(expected.CreatedAt).Equal(
			postgresAdminTime(next.CreatedAt),
		) ||
		!postgresAdminTime(next.UpdatedAt).After(
			postgresAdminTime(expected.UpdatedAt),
		) {
		return ports.ErrStoreContractViolation
	}

	switch action {
	case domain.AuditActionResellerUpdate:
		if expected.BalanceCents != next.BalanceCents ||
			expected.ReservedCents != next.ReservedCents {
			return ports.ErrStoreContractViolation
		}
		return validateResellerEnabledTransition(expected, next, true)

	case domain.AuditActionResellerEnable:
		if !sameResellerExceptEnabledState(expected, next) ||
			expected.Enabled ||
			!next.Enabled ||
			next.DisabledAt != nil {
			return ports.ErrStoreContractViolation
		}
		return nil

	case domain.AuditActionResellerDisable:
		if !sameResellerExceptEnabledState(expected, next) ||
			!expected.Enabled ||
			next.Enabled ||
			next.DisabledAt == nil ||
			!postgresAdminTime(*next.DisabledAt).Equal(
				postgresAdminTime(next.UpdatedAt),
			) {
			return ports.ErrStoreContractViolation
		}
		return nil

	case
		domain.AuditActionResellerBalanceAdjust,
		domain.AuditActionResellerBalanceSet:
		if !sameResellerExceptBalance(expected, next) {
			return ports.ErrStoreContractViolation
		}
		return nil

	default:
		return ports.ErrStoreContractViolation
	}
}

func validateResellerEnabledTransition(
	expected domain.Reseller,
	next domain.Reseller,
	allowUnchanged bool,
) error {
	if expected.Enabled == next.Enabled {
		if !allowUnchanged ||
			!sameAdminTimePointer(
				expected.DisabledAt,
				next.DisabledAt,
			) {
			return ports.ErrStoreContractViolation
		}
		return nil
	}
	if expected.Enabled && !next.Enabled {
		if next.DisabledAt == nil ||
			!postgresAdminTime(*next.DisabledAt).Equal(
				postgresAdminTime(next.UpdatedAt),
			) {
			return ports.ErrStoreContractViolation
		}
		return nil
	}
	if !expected.Enabled && next.Enabled && next.DisabledAt == nil {
		return nil
	}
	return ports.ErrStoreContractViolation
}

func sameResellerExceptEnabledState(
	expected domain.Reseller,
	next domain.Reseller,
) bool {
	return expected.ID == next.ID &&
		expected.Name == next.Name &&
		expected.ProviderType == next.ProviderType &&
		expected.BaseURL == next.BaseURL &&
		expected.APIKeyEnv == next.APIKeyEnv &&
		expected.BalanceCents == next.BalanceCents &&
		expected.ReservedCents == next.ReservedCents &&
		expected.MinimumBalanceCents == next.MinimumBalanceCents &&
		postgresAdminTime(expected.CreatedAt).Equal(
			postgresAdminTime(next.CreatedAt),
		)
}

func sameResellerExceptBalance(
	expected domain.Reseller,
	next domain.Reseller,
) bool {
	return expected.ID == next.ID &&
		expected.Name == next.Name &&
		expected.ProviderType == next.ProviderType &&
		expected.BaseURL == next.BaseURL &&
		expected.APIKeyEnv == next.APIKeyEnv &&
		expected.Enabled == next.Enabled &&
		expected.ReservedCents == next.ReservedCents &&
		expected.MinimumBalanceCents == next.MinimumBalanceCents &&
		postgresAdminTime(expected.CreatedAt).Equal(
			postgresAdminTime(next.CreatedAt),
		) &&
		sameAdminTimePointer(expected.DisabledAt, next.DisabledAt)
}

func sameAdminReseller(
	left domain.Reseller,
	right domain.Reseller,
) bool {
	return left.ID == right.ID &&
		left.Name == right.Name &&
		left.ProviderType == right.ProviderType &&
		left.BaseURL == right.BaseURL &&
		left.APIKeyEnv == right.APIKeyEnv &&
		left.Enabled == right.Enabled &&
		left.BalanceCents == right.BalanceCents &&
		left.ReservedCents == right.ReservedCents &&
		left.MinimumBalanceCents == right.MinimumBalanceCents &&
		postgresAdminTime(left.CreatedAt).Equal(
			postgresAdminTime(right.CreatedAt),
		) &&
		postgresAdminTime(left.UpdatedAt).Equal(
			postgresAdminTime(right.UpdatedAt),
		) &&
		sameAdminTimePointer(left.DisabledAt, right.DisabledAt)
}

func canonicalAdminReseller(value domain.Reseller) domain.Reseller {
	result := value
	result.CreatedAt = postgresAdminTime(value.CreatedAt)
	result.UpdatedAt = postgresAdminTime(value.UpdatedAt)
	if value.DisabledAt != nil {
		disabledAt := postgresAdminTime(*value.DisabledAt)
		result.DisabledAt = &disabledAt
	}
	return result
}

func postgresAdminTime(value time.Time) time.Time {
	return value.UTC().Truncate(time.Microsecond)
}

func adminResellerTimeArg(value *time.Time) any {
	if value == nil {
		return nil
	}
	return postgresAdminTime(*value)
}

func adminResellerApplicationState(
	value domain.Reseller,
) domain.AuditState {
	return domain.AuditState{
		"id":                    value.ID,
		"name":                  value.Name,
		"provider_type":         value.ProviderType,
		"base_url":              value.BaseURL,
		"api_key_env":           value.APIKeyEnv,
		"enabled":               value.Enabled,
		"balance_cents":         value.BalanceCents,
		"reserved_cents":        value.ReservedCents,
		"minimum_balance_cents": value.MinimumBalanceCents,
		"created_at":            value.CreatedAt,
		"updated_at":            value.UpdatedAt,
		"disabled_at":           value.DisabledAt,
	}
}

func adminResellerState(value domain.Reseller) domain.AuditState {
	canonical := canonicalAdminReseller(value)
	return domain.AuditState{
		"id":                    canonical.ID,
		"name":                  canonical.Name,
		"provider_type":         canonical.ProviderType,
		"base_url":              canonical.BaseURL,
		"api_key_env":           canonical.APIKeyEnv,
		"enabled":               canonical.Enabled,
		"balance_cents":         canonical.BalanceCents,
		"reserved_cents":        canonical.ReservedCents,
		"minimum_balance_cents": canonical.MinimumBalanceCents,
		"created_at":            canonical.CreatedAt,
		"updated_at":            canonical.UpdatedAt,
		"disabled_at":           canonical.DisabledAt,
	}
}

func canonicalResellerAudit(
	audit domain.AuditContext,
	before domain.AuditState,
	after domain.AuditState,
	at time.Time,
) domain.AuditContext {
	result := audit
	result.BeforeState = before
	result.AfterState = after
	result.CreatedAt = postgresAdminTime(at)
	return result
}

func buildAdminResellerFilter(
	filter ports.ResellerListFilter,
) (string, []any) {
	var clauses []string
	var args []any

	if filter.ProviderType != "" {
		args = append(args, string(filter.ProviderType))
		clauses = append(
			clauses,
			fmt.Sprintf("provider_type = $%d", len(args)),
		)
	}
	if filter.Enabled != nil {
		args = append(args, *filter.Enabled)
		clauses = append(
			clauses,
			fmt.Sprintf("enabled = $%d", len(args)),
		)
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}
