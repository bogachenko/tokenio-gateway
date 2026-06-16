package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const telegramDeliveryAttemptColumns = `
    id,
    alert_id,
    attempt_number,
    status,
    attempt_state,
    failure_code,
    started_at,
    completed_at
`

const findTelegramDeliveryAttemptForUpdateSQL = `
SELECT
` + telegramDeliveryAttemptColumns + `
FROM tokenio_telegram_delivery_attempts
WHERE alert_id = $1
  AND attempt_number = $2
FOR UPDATE
`

const insertTelegramDeliveryAttemptSQL = `
INSERT INTO tokenio_telegram_delivery_attempts (
` + telegramDeliveryAttemptColumns + `
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
RETURNING
` + telegramDeliveryAttemptColumns

const completeTelegramDeliveryAttemptSQL = `
UPDATE tokenio_telegram_delivery_attempts
SET
    status = $3,
    attempt_state = $4,
    failure_code = $5,
    completed_at = $6
WHERE alert_id = $1
  AND attempt_number = $2
  AND status = 'started'
RETURNING
` + telegramDeliveryAttemptColumns

const loadTelegramDeliveryAttemptsSQL = `
SELECT
` + telegramDeliveryAttemptColumns + `
FROM tokenio_telegram_delivery_attempts
WHERE alert_id = $1
ORDER BY attempt_number ASC
LIMIT $2
`

const loadStartedTelegramDeliveryAttemptsBeforeSQL = `
SELECT
` + telegramDeliveryAttemptColumns + `
FROM tokenio_telegram_delivery_attempts
WHERE status = 'started'
  AND started_at < $1
ORDER BY
    started_at ASC,
    alert_id ASC,
    attempt_number ASC
LIMIT $2
`

type TelegramDeliveryAttemptStore struct {
	db *DB
}

var _ ports.TelegramDeliveryAttemptStore = (*TelegramDeliveryAttemptStore)(nil)

func NewTelegramDeliveryAttemptStore(
	db *DB,
) (*TelegramDeliveryAttemptStore, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &TelegramDeliveryAttemptStore{db: db}, nil
}

func (s *TelegramDeliveryAttemptStore) StartTelegramDeliveryAttempt(
	ctx context.Context,
	attempt domain.TelegramDeliveryAttempt,
) (domain.TelegramDeliveryAttempt, error) {
	if s == nil || s.db == nil || s.db.pool == nil {
		return domain.TelegramDeliveryAttempt{}, ErrInvalidDatabaseConfig
	}
	if ctx == nil {
		return domain.TelegramDeliveryAttempt{},
			ports.ErrStoreContractViolation
	}
	if err := validateStartedTelegramDeliveryAttempt(attempt); err != nil {
		return domain.TelegramDeliveryAttempt{}, err
	}

	attempt = canonicalTelegramDeliveryAttempt(attempt)
	var result domain.TelegramDeliveryAttempt
	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{},
		func(tx pgx.Tx) error {
			if err := lockTelegramDeliveryAttempt(
				ctx,
				tx,
				attempt.AlertID,
				attempt.AttemptNumber,
			); err != nil {
				return err
			}

			existing, err := scanTelegramDeliveryAttempt(
				tx.QueryRow(
					ctx,
					findTelegramDeliveryAttemptForUpdateSQL,
					attempt.AlertID,
					attempt.AttemptNumber,
				),
			)
			switch {
			case err == nil:
				if !telegramDeliveryAttemptsEqual(existing, attempt) {
					return ports.ErrStoreConflict
				}
				result = existing
				return nil
			case errors.Is(err, ports.ErrNotFound):
			default:
				return err
			}

			inserted, err := scanTelegramDeliveryAttempt(
				tx.QueryRow(
					ctx,
					insertTelegramDeliveryAttemptSQL,
					telegramDeliveryAttemptArgs(attempt)...,
				),
			)
			if err != nil {
				return err
			}
			if !telegramDeliveryAttemptsEqual(inserted, attempt) {
				return ports.ErrStoreContractViolation
			}
			result = inserted
			return nil
		},
	)
	if err != nil {
		return domain.TelegramDeliveryAttempt{}, err
	}
	return result, nil
}

func (s *TelegramDeliveryAttemptStore) CompleteTelegramDeliveryAttempt(
	ctx context.Context,
	attempt domain.TelegramDeliveryAttempt,
) (domain.TelegramDeliveryAttempt, error) {
	if s == nil || s.db == nil || s.db.pool == nil {
		return domain.TelegramDeliveryAttempt{}, ErrInvalidDatabaseConfig
	}
	if ctx == nil {
		return domain.TelegramDeliveryAttempt{},
			ports.ErrStoreContractViolation
	}
	if err := validateTerminalTelegramDeliveryAttempt(attempt); err != nil {
		return domain.TelegramDeliveryAttempt{}, err
	}

	attempt = canonicalTelegramDeliveryAttempt(attempt)
	var result domain.TelegramDeliveryAttempt
	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{},
		func(tx pgx.Tx) error {
			if err := lockTelegramDeliveryAttempt(
				ctx,
				tx,
				attempt.AlertID,
				attempt.AttemptNumber,
			); err != nil {
				return err
			}

			current, err := scanTelegramDeliveryAttempt(
				tx.QueryRow(
					ctx,
					findTelegramDeliveryAttemptForUpdateSQL,
					attempt.AlertID,
					attempt.AttemptNumber,
				),
			)
			if err != nil {
				return err
			}

			if current.Status !=
				domain.TelegramDeliveryAttemptStatusStarted {
				if telegramDeliveryAttemptsEqual(current, attempt) {
					result = current
					return nil
				}
				return ports.ErrStoreConflict
			}
			if !sameTelegramDeliveryAttemptIdentity(current, attempt) {
				return ports.ErrStoreContractViolation
			}

			updated, err := scanTelegramDeliveryAttempt(
				tx.QueryRow(
					ctx,
					completeTelegramDeliveryAttemptSQL,
					attempt.AlertID,
					attempt.AttemptNumber,
					string(attempt.Status),
					nullableTelegramDeliveryAttemptState(
						attempt.AttemptState,
					),
					nullableString(attempt.FailureCode),
					attempt.CompletedAt,
				),
			)
			if err != nil {
				return err
			}
			if !telegramDeliveryAttemptsEqual(updated, attempt) {
				return ports.ErrStoreContractViolation
			}
			result = updated
			return nil
		},
	)
	if err != nil {
		return domain.TelegramDeliveryAttempt{}, err
	}
	return result, nil
}

func (s *TelegramDeliveryAttemptStore) LoadTelegramDeliveryAttempts(
	ctx context.Context,
	alertID string,
	limit int,
) ([]domain.TelegramDeliveryAttempt, error) {
	if s == nil || s.db == nil || s.db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	if ctx == nil ||
		strings.TrimSpace(alertID) == "" ||
		limit <= 0 {
		return nil, ports.ErrStoreContractViolation
	}

	rows, err := s.db.Query(
		ctx,
		loadTelegramDeliveryAttemptsSQL,
		alertID,
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]domain.TelegramDeliveryAttempt, 0, limit)
	previousNumber := 0
	for rows.Next() {
		attempt, err := scanTelegramDeliveryAttempt(rows)
		if err != nil {
			return nil, err
		}
		if attempt.AlertID != alertID ||
			attempt.AttemptNumber <= previousNumber {
			return nil, ports.ErrStoreContractViolation
		}
		result = append(result, attempt)
		previousNumber = attempt.AttemptNumber
	}
	if err := rows.Err(); err != nil {
		return nil, NormalizeError(err)
	}
	if len(result) > limit {
		return nil, ports.ErrStoreContractViolation
	}
	return result, nil
}

func (
	s *TelegramDeliveryAttemptStore,
) LoadStartedTelegramDeliveryAttemptsBefore(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) ([]domain.TelegramDeliveryAttempt, error) {
	if s == nil || s.db == nil || s.db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	if ctx == nil ||
		cutoff.IsZero() ||
		cutoff.Location() != time.UTC ||
		limit <= 0 {
		return nil, ports.ErrStoreContractViolation
	}

	rows, err := s.db.Query(
		ctx,
		loadStartedTelegramDeliveryAttemptsBeforeSQL,
		operationalTime(cutoff),
		limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]domain.TelegramDeliveryAttempt, 0, limit)
	var previous *domain.TelegramDeliveryAttempt
	for rows.Next() {
		attempt, err := scanTelegramDeliveryAttempt(rows)
		if err != nil {
			return nil, err
		}
		if attempt.Status !=
			domain.TelegramDeliveryAttemptStatusStarted ||
			!attempt.StartedAt.Before(cutoff) {
			return nil, ports.ErrStoreContractViolation
		}
		if previous != nil &&
			!telegramDeliveryRecoveryOrderBefore(*previous, attempt) {
			return nil, ports.ErrStoreContractViolation
		}
		result = append(result, attempt)
		copyAttempt := attempt
		previous = &copyAttempt
	}
	if err := rows.Err(); err != nil {
		return nil, NormalizeError(err)
	}
	if len(result) > limit {
		return nil, ports.ErrStoreContractViolation
	}
	return result, nil
}

func telegramDeliveryRecoveryOrderBefore(
	left domain.TelegramDeliveryAttempt,
	right domain.TelegramDeliveryAttempt,
) bool {
	switch {
	case left.StartedAt.Before(right.StartedAt):
		return true
	case right.StartedAt.Before(left.StartedAt):
		return false
	case left.AlertID < right.AlertID:
		return true
	case left.AlertID > right.AlertID:
		return false
	default:
		return left.AttemptNumber < right.AttemptNumber
	}
}

func lockTelegramDeliveryAttempt(
	ctx context.Context,
	tx pgx.Tx,
	alertID string,
	attemptNumber int,
) error {
	_, err := tx.Exec(
		ctx,
		`
SELECT pg_advisory_xact_lock(
    hashtextextended(
        'tokenio_telegram_delivery_attempt:' || $1 || ':' || ($2::integer)::text,
        0
    )
)
`,
		alertID,
		attemptNumber,
	)
	return NormalizeError(err)
}

func telegramDeliveryAttemptArgs(
	attempt domain.TelegramDeliveryAttempt,
) []any {
	return []any{
		attempt.ID,
		attempt.AlertID,
		attempt.AttemptNumber,
		string(attempt.Status),
		nullableTelegramDeliveryAttemptState(attempt.AttemptState),
		nullableString(attempt.FailureCode),
		attempt.StartedAt,
		attempt.CompletedAt,
	}
}

func scanTelegramDeliveryAttempt(
	row interface{ Scan(...any) error },
) (domain.TelegramDeliveryAttempt, error) {
	var attempt domain.TelegramDeliveryAttempt
	var status string
	var attemptState pgtype.Text
	var failureCode pgtype.Text
	var completedAt pgtype.Timestamptz

	err := row.Scan(
		&attempt.ID,
		&attempt.AlertID,
		&attempt.AttemptNumber,
		&status,
		&attemptState,
		&failureCode,
		&attempt.StartedAt,
		&completedAt,
	)
	if err != nil {
		return domain.TelegramDeliveryAttempt{}, NormalizeError(err)
	}

	attempt.Status = domain.TelegramDeliveryAttemptStatus(status)
	if attemptState.Valid {
		attempt.AttemptState =
			domain.TelegramDeliveryAttemptState(attemptState.String)
	}
	if failureCode.Valid {
		attempt.FailureCode = failureCode.String
	}
	if completedAt.Valid {
		value := completedAt.Time
		attempt.CompletedAt = &value
	}
	attempt = canonicalTelegramDeliveryAttempt(attempt)

	if err := validatePersistedTelegramDeliveryAttempt(attempt); err != nil {
		return domain.TelegramDeliveryAttempt{}, err
	}
	return attempt, nil
}

func validatePersistedTelegramDeliveryAttempt(
	attempt domain.TelegramDeliveryAttempt,
) error {
	switch attempt.Status {
	case domain.TelegramDeliveryAttemptStatusStarted:
		return validateStartedTelegramDeliveryAttempt(attempt)
	case domain.TelegramDeliveryAttemptStatusSucceeded,
		domain.TelegramDeliveryAttemptStatusFailed:
		return validateTerminalTelegramDeliveryAttempt(attempt)
	default:
		return ports.ErrStoreContractViolation
	}
}

func validateStartedTelegramDeliveryAttempt(
	attempt domain.TelegramDeliveryAttempt,
) error {
	if !validTelegramDeliveryAttemptIdentity(attempt) ||
		attempt.Status != domain.TelegramDeliveryAttemptStatusStarted ||
		attempt.AttemptState != "" ||
		strings.TrimSpace(attempt.FailureCode) != "" ||
		attempt.StartedAt.IsZero() ||
		attempt.StartedAt.Location() != time.UTC ||
		attempt.CompletedAt != nil {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validateTerminalTelegramDeliveryAttempt(
	attempt domain.TelegramDeliveryAttempt,
) error {
	if !validTelegramDeliveryAttemptIdentity(attempt) ||
		attempt.StartedAt.IsZero() ||
		attempt.StartedAt.Location() != time.UTC ||
		attempt.CompletedAt == nil ||
		attempt.CompletedAt.IsZero() ||
		attempt.CompletedAt.Location() != time.UTC ||
		attempt.CompletedAt.Before(attempt.StartedAt) {
		return ports.ErrStoreContractViolation
	}

	switch attempt.Status {
	case domain.TelegramDeliveryAttemptStatusSucceeded:
		if attempt.AttemptState !=
			domain.TelegramDeliveryAttemptStateResponseReceived ||
			strings.TrimSpace(attempt.FailureCode) != "" {
			return ports.ErrStoreContractViolation
		}
	case domain.TelegramDeliveryAttemptStatusFailed:
		if !validTelegramDeliveryAttemptState(attempt.AttemptState) ||
			strings.TrimSpace(attempt.FailureCode) == "" {
			return ports.ErrStoreContractViolation
		}
	default:
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validTelegramDeliveryAttemptIdentity(
	attempt domain.TelegramDeliveryAttempt,
) bool {
	return strings.TrimSpace(attempt.ID) != "" &&
		strings.TrimSpace(attempt.AlertID) != "" &&
		attempt.AttemptNumber > 0
}

func validTelegramDeliveryAttemptState(
	state domain.TelegramDeliveryAttemptState,
) bool {
	switch state {
	case domain.TelegramDeliveryAttemptStateNotSent,
		domain.TelegramDeliveryAttemptStateSentNoResponse,
		domain.TelegramDeliveryAttemptStateResponseReceived:
		return true
	default:
		return false
	}
}

func sameTelegramDeliveryAttemptIdentity(
	left domain.TelegramDeliveryAttempt,
	right domain.TelegramDeliveryAttempt,
) bool {
	return left.ID == right.ID &&
		left.AlertID == right.AlertID &&
		left.AttemptNumber == right.AttemptNumber &&
		left.StartedAt.Equal(right.StartedAt)
}

func telegramDeliveryAttemptsEqual(
	left domain.TelegramDeliveryAttempt,
	right domain.TelegramDeliveryAttempt,
) bool {
	return sameTelegramDeliveryAttemptIdentity(left, right) &&
		left.Status == right.Status &&
		left.AttemptState == right.AttemptState &&
		left.FailureCode == right.FailureCode &&
		equalTimePointers(left.CompletedAt, right.CompletedAt)
}

func canonicalTelegramDeliveryAttempt(
	value domain.TelegramDeliveryAttempt,
) domain.TelegramDeliveryAttempt {
	value.StartedAt = operationalTime(value.StartedAt)
	value.CompletedAt = operationalTimePointer(value.CompletedAt)
	return value
}

func nullableTelegramDeliveryAttemptState(
	value domain.TelegramDeliveryAttemptState,
) any {
	if value == "" {
		return nil
	}
	return string(value)
}
