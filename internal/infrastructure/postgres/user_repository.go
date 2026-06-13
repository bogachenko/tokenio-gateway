package postgres

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const findUserByIDSQL = `
SELECT
    id,
    external_billing_user_id,
    email,
    name,
    enabled,
    created_at,
    updated_at,
    disabled_at
FROM tokenio_users
WHERE id = $1
`

type UserRepository struct {
	db DBTX
}

var _ ports.UserRepository = (*UserRepository)(nil)

func NewUserRepository(db DBTX) (*UserRepository, error) {
	if db == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &UserRepository{db: db}, nil
}

func (r *UserRepository) FindByID(
	ctx context.Context,
	userID string,
) (*domain.User, error) {
	user, err := scanUser(r.db.QueryRow(ctx, findUserByIDSQL, userID))
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func scanUser(row pgx.Row) (domain.User, error) {
	var value domain.User
	var email pgtype.Text
	var name pgtype.Text
	var disabledAt pgtype.Timestamptz

	if err := row.Scan(
		&value.ID,
		&value.ExternalBillingUserID,
		&email,
		&name,
		&value.Enabled,
		&value.CreatedAt,
		&value.UpdatedAt,
		&disabledAt,
	); err != nil {
		return domain.User{}, normalizeRegistryReadError(err)
	}

	value.Email = optionalText(email)
	value.Name = optionalText(name)
	value.CreatedAt = canonicalTime(value.CreatedAt)
	value.UpdatedAt = canonicalTime(value.UpdatedAt)
	value.DisabledAt = optionalTime(disabledAt)
	return value, nil
}
