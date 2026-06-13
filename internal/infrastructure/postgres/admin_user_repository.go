package postgres

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
)

const adminUserColumns = `
    id,
    external_billing_user_id,
    email,
    name,
    enabled,
    created_at,
    updated_at,
    disabled_at
`

const findAdminUserByIDSQL = `
SELECT
` + adminUserColumns + `
FROM tokenio_users
WHERE id = $1
`

const findAdminUserByIDForUpdateSQL = `
SELECT
` + adminUserColumns + `
FROM tokenio_users
WHERE id = $1
FOR UPDATE
`

const insertAdminUserSQL = `
INSERT INTO tokenio_users (
    id,
    external_billing_user_id,
    email,
    name,
    enabled,
    created_at,
    updated_at,
    disabled_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING
` + adminUserColumns

const updateAdminUserSQL = `
UPDATE tokenio_users
SET
    external_billing_user_id = $2,
    email = $3,
    name = $4,
    enabled = $5,
    created_at = $6,
    updated_at = $7,
    disabled_at = $8
WHERE id = $1
RETURNING
` + adminUserColumns

type AdminUserRepository struct {
	db *DB
}

var _ ports.AdminUserRepository = (*AdminUserRepository)(nil)

func NewAdminUserRepository(db *DB) (*AdminUserRepository, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &AdminUserRepository{db: db}, nil
}

func (r *AdminUserRepository) FindByID(
	ctx context.Context,
	userID string,
) (*domain.User, error) {
	user, err := scanUser(
		r.db.QueryRow(ctx, findAdminUserByIDSQL, userID),
	)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *AdminUserRepository) ListUsers(
	ctx context.Context,
	filter ports.UserListFilter,
) (ports.Page[domain.User], error) {
	if err := validateAdminPage(filter.Page); err != nil {
		return ports.Page[domain.User]{}, err
	}

	where, args := buildUserFilter(filter)
	var result ports.Page[domain.User]

	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{
			IsoLevel:   pgx.RepeatableRead,
			AccessMode: pgx.ReadOnly,
		},
		func(tx pgx.Tx) error {
			countSQL := "SELECT COUNT(*) FROM tokenio_users" + where
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
` + adminUserColumns + `
FROM tokenio_users` + where + fmt.Sprintf(`
ORDER BY created_at DESC, id ASC
LIMIT $%d OFFSET $%d
`, limitPosition, offsetPosition)

			rows, err := tx.Query(ctx, listSQL, listArgs...)
			if err != nil {
				return normalizeRegistryReadError(err)
			}
			defer rows.Close()

			result.Items = make([]domain.User, 0)
			for rows.Next() {
				user, err := scanUser(rows)
				if err != nil {
					return err
				}
				result.Items = append(result.Items, user)
			}
			if err := rows.Err(); err != nil {
				return normalizeRegistryReadError(err)
			}
			return nil
		},
	)
	if err != nil {
		return ports.Page[domain.User]{}, err
	}
	return result, nil
}

func (r *AdminUserRepository) CreateUserWithAudit(
	ctx context.Context,
	requested domain.User,
	audit domain.AuditContext,
) (domain.User, error) {
	if err := validateAdminUserRecord(requested); err != nil {
		return domain.User{}, err
	}
	if !requested.CreatedAt.Equal(requested.UpdatedAt) ||
		!requested.Enabled ||
		requested.DisabledAt != nil {
		return domain.User{}, ports.ErrStoreContractViolation
	}
	if err := validateAuditForEntity(
		audit,
		domain.AuditActionUserCreate,
		"user",
		requested.ID,
		domain.AuditState{},
		adminUserState(requested),
		requested.CreatedAt,
	); err != nil {
		return domain.User{}, err
	}

	var created domain.User
	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			value, err := scanUser(tx.QueryRow(
				ctx,
				insertAdminUserSQL,
				requested.ID,
				requested.ExternalBillingUserID,
				nullIfEmpty(requested.Email),
				nullIfEmpty(requested.Name),
				requested.Enabled,
				requested.CreatedAt,
				requested.UpdatedAt,
				canonicalTimePointer(requested.DisabledAt),
			))
			if err != nil {
				return normalizeAdminWriteError(err)
			}
			if !sameUserPersistence(value, requested) {
				return ports.ErrStoreContractViolation
			}
			if err := insertAdminAudit(ctx, tx, audit); err != nil {
				return err
			}
			created = value
			return nil
		},
	)
	if err != nil {
		if errors.Is(err, ports.ErrStoreConflict) {
			return domain.User{}, ports.ErrAdminConflict
		}
		return domain.User{}, err
	}
	return created, nil
}

func (r *AdminUserRepository) CompareAndSwapUserWithAudit(
	ctx context.Context,
	expected domain.User,
	next domain.User,
	audit domain.AuditContext,
) (domain.User, error) {
	if err := validateAdminUserRecord(expected); err != nil {
		return domain.User{}, err
	}
	if err := validateAdminUserRecord(next); err != nil {
		return domain.User{}, err
	}
	if expected.ID != next.ID ||
		expected.ExternalBillingUserID != next.ExternalBillingUserID ||
		expected.Email != next.Email ||
		expected.Name != next.Name ||
		!expected.CreatedAt.Equal(next.CreatedAt) ||
		!next.UpdatedAt.After(expected.UpdatedAt) {
		return domain.User{}, ports.ErrStoreContractViolation
	}

	action := domain.AuditActionUserDisable
	switch {
	case expected.Enabled && !next.Enabled:
		if next.DisabledAt == nil ||
			!next.DisabledAt.Equal(next.UpdatedAt) {
			return domain.User{}, ports.ErrStoreContractViolation
		}
	case !expected.Enabled && next.Enabled:
		action = domain.AuditActionUserEnable
		if next.DisabledAt != nil {
			return domain.User{}, ports.ErrStoreContractViolation
		}
	default:
		return domain.User{}, ports.ErrStoreContractViolation
	}

	if err := validateAuditForEntity(
		audit,
		action,
		"user",
		next.ID,
		adminUserState(expected),
		adminUserState(next),
		next.UpdatedAt,
	); err != nil {
		return domain.User{}, err
	}

	var updated domain.User
	err := InTx(
		ctx,
		r.db,
		pgx.TxOptions{IsoLevel: pgx.Serializable},
		func(tx pgx.Tx) error {
			current, err := scanUser(tx.QueryRow(
				ctx,
				findAdminUserByIDForUpdateSQL,
				expected.ID,
			))
			if err != nil {
				return err
			}
			if !sameUserPersistence(current, expected) {
				return ports.ErrAdminStateConflict
			}

			value, err := scanUser(tx.QueryRow(
				ctx,
				updateAdminUserSQL,
				next.ID,
				next.ExternalBillingUserID,
				nullIfEmpty(next.Email),
				nullIfEmpty(next.Name),
				next.Enabled,
				next.CreatedAt,
				next.UpdatedAt,
				canonicalTimePointer(next.DisabledAt),
			))
			if err != nil {
				return normalizeAdminWriteError(err)
			}
			if !sameUserPersistence(value, next) {
				return ports.ErrStoreContractViolation
			}
			if err := insertAdminAudit(ctx, tx, audit); err != nil {
				return err
			}
			updated = value
			return nil
		},
	)
	if err != nil {
		if errors.Is(err, ports.ErrStoreConflict) {
			return domain.User{}, ports.ErrAdminStateConflict
		}
		return domain.User{}, err
	}
	return updated, nil
}

func validateAdminUserRecord(value domain.User) error {
	if value.ID == "" ||
		value.ExternalBillingUserID == "" ||
		!isAdminUTCTime(value.CreatedAt) ||
		!isAdminUTCTime(value.UpdatedAt) ||
		value.UpdatedAt.Before(value.CreatedAt) ||
		value.DisabledAt != nil &&
			!isAdminUTCTime(*value.DisabledAt) {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func buildUserFilter(filter ports.UserListFilter) (string, []any) {
	var clauses []string
	var args []any

	if filter.Enabled != nil {
		args = append(args, *filter.Enabled)
		clauses = append(
			clauses,
			fmt.Sprintf("enabled = $%d", len(args)),
		)
	}
	if filter.Email != "" {
		args = append(args, filter.Email)
		clauses = append(
			clauses,
			fmt.Sprintf("email = $%d", len(args)),
		)
	}

	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}
