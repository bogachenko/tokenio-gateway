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

const forwardingAttemptColumns = `
    local_request_id,
    attempt_number,
    route_id,
    reseller_id,
    api_family,
    endpoint_kind,
    client_model,
    provider_type,
    provider_model,
    status,
    attempt_state,
    upstream_status_code,
    failure_kind,
    route_retry_candidate,
    started_at,
    completed_at
`

const findForwardingAttemptForUpdateSQL = `
SELECT
` + forwardingAttemptColumns + `
FROM tokenio_forwarding_attempts
WHERE local_request_id = $1
  AND attempt_number = $2
FOR UPDATE
`

const insertForwardingAttemptSQL = `
INSERT INTO tokenio_forwarding_attempts (
` + forwardingAttemptColumns + `
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8,
    $9, $10, $11, $12, $13, $14, $15, $16
)
`

const completeForwardingAttemptSQL = `
UPDATE tokenio_forwarding_attempts
SET
    status = $3,
    attempt_state = $4,
    upstream_status_code = $5,
    failure_kind = $6,
    route_retry_candidate = $7,
    completed_at = $8
WHERE local_request_id = $1
  AND attempt_number = $2
  AND status = 'started'
`

const loadForwardingAttemptsSQL = `
SELECT
` + forwardingAttemptColumns + `
FROM tokenio_forwarding_attempts
WHERE local_request_id = $1
ORDER BY attempt_number ASC
`

const loadStartedForwardingAttemptsBeforeSQL = `
SELECT
` + forwardingAttemptColumns + `
FROM tokenio_forwarding_attempts
WHERE status = 'started'
  AND started_at < $1
ORDER BY
    started_at ASC,
    local_request_id ASC,
    attempt_number ASC
LIMIT $2
`

type ForwardingAttemptStore struct {
	db *DB
}

var _ ports.ForwardingAttemptStore = (*ForwardingAttemptStore)(nil)

func NewForwardingAttemptStore(
	db *DB,
) (*ForwardingAttemptStore, error) {
	if db == nil || db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	return &ForwardingAttemptStore{db: db}, nil
}

func (s *ForwardingAttemptStore) StartAttempt(
	ctx context.Context,
	attempt domain.ForwardingAttempt,
) (domain.ForwardingAttempt, error) {
	if s == nil || s.db == nil || s.db.pool == nil {
		return domain.ForwardingAttempt{}, ErrInvalidDatabaseConfig
	}
	if ctx == nil {
		return domain.ForwardingAttempt{}, ports.ErrStoreContractViolation
	}
	if err := validateStartedForwardingAttempt(attempt); err != nil {
		return domain.ForwardingAttempt{}, err
	}

	var result domain.ForwardingAttempt
	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{},
		func(tx pgx.Tx) error {
			if err := lockForwardingAttempt(
				ctx,
				tx,
				attempt.LocalRequestID,
				attempt.AttemptNumber,
			); err != nil {
				return err
			}

			existing, err := scanForwardingAttempt(
				tx.QueryRow(
					ctx,
					findForwardingAttemptForUpdateSQL,
					attempt.LocalRequestID,
					attempt.AttemptNumber,
				),
			)
			switch {
			case err == nil:
				if !forwardingAttemptsEqual(existing, attempt) {
					return ports.ErrStoreConflict
				}
				result = existing
				return nil
			case errors.Is(err, ports.ErrNotFound):
			default:
				return err
			}

			_, err = tx.Exec(
				ctx,
				insertForwardingAttemptSQL,
				forwardingAttemptInsertArgs(attempt)...,
			)
			if err != nil {
				return NormalizeError(err)
			}
			result = attempt
			return nil
		},
	)
	if err != nil {
		return domain.ForwardingAttempt{}, err
	}
	return result, nil
}

func (s *ForwardingAttemptStore) CompleteAttempt(
	ctx context.Context,
	attempt domain.ForwardingAttempt,
) (domain.ForwardingAttempt, error) {
	if s == nil || s.db == nil || s.db.pool == nil {
		return domain.ForwardingAttempt{}, ErrInvalidDatabaseConfig
	}
	if ctx == nil {
		return domain.ForwardingAttempt{}, ports.ErrStoreContractViolation
	}
	if err := validateTerminalForwardingAttempt(attempt); err != nil {
		return domain.ForwardingAttempt{}, err
	}

	var result domain.ForwardingAttempt
	err := InTx(
		ctx,
		s.db,
		pgx.TxOptions{},
		func(tx pgx.Tx) error {
			if err := lockForwardingAttempt(
				ctx,
				tx,
				attempt.LocalRequestID,
				attempt.AttemptNumber,
			); err != nil {
				return err
			}

			current, err := scanForwardingAttempt(
				tx.QueryRow(
					ctx,
					findForwardingAttemptForUpdateSQL,
					attempt.LocalRequestID,
					attempt.AttemptNumber,
				),
			)
			if err != nil {
				return err
			}

			if current.Status != domain.ForwardingAttemptStatusStarted {
				if forwardingAttemptsEqual(current, attempt) {
					result = current
					return nil
				}
				return ports.ErrStoreConflict
			}
			if !sameForwardingAttemptIdentity(current, attempt) {
				return ports.ErrStoreContractViolation
			}

			tag, err := tx.Exec(
				ctx,
				completeForwardingAttemptSQL,
				attempt.LocalRequestID,
				attempt.AttemptNumber,
				string(attempt.Status),
				nullableAttemptState(attempt.AttemptState),
				nullableStatusCode(attempt.UpstreamStatusCode),
				nullableString(attempt.FailureKind),
				attempt.RouteRetryCandidate,
				attempt.CompletedAt,
			)
			if err != nil {
				return NormalizeError(err)
			}
			if tag.RowsAffected() != 1 {
				return ports.ErrStoreConflict
			}
			result = attempt
			return nil
		},
	)
	if err != nil {
		return domain.ForwardingAttempt{}, err
	}
	return result, nil
}

func (s *ForwardingAttemptStore) LoadAttempts(
	ctx context.Context,
	localRequestID string,
) ([]domain.ForwardingAttempt, error) {
	if s == nil || s.db == nil || s.db.pool == nil {
		return nil, ErrInvalidDatabaseConfig
	}
	if ctx == nil || strings.TrimSpace(localRequestID) == "" {
		return nil, ports.ErrStoreContractViolation
	}

	rows, err := s.db.Query(
		ctx,
		loadForwardingAttemptsSQL,
		localRequestID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make([]domain.ForwardingAttempt, 0)
	expectedNumber := 1
	for rows.Next() {
		attempt, err := scanForwardingAttempt(rows)
		if err != nil {
			return nil, err
		}
		if attempt.LocalRequestID != localRequestID ||
			attempt.AttemptNumber != expectedNumber {
			return nil, ports.ErrStoreContractViolation
		}
		result = append(result, attempt)
		expectedNumber++
	}
	if err := rows.Err(); err != nil {
		return nil, NormalizeError(err)
	}
	return result, nil
}

func (s *ForwardingAttemptStore) LoadStartedBefore(
	ctx context.Context,
	cutoff time.Time,
	limit int,
) ([]domain.ForwardingAttempt, error) {
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
		loadStartedForwardingAttemptsBeforeSQL,
		cutoff,
		limit,
	)
	if err != nil {
		return nil, NormalizeError(err)
	}
	defer rows.Close()

	result := make([]domain.ForwardingAttempt, 0, limit)
	var previous *domain.ForwardingAttempt
	for rows.Next() {
		attempt, err := scanForwardingAttempt(rows)
		if err != nil {
			return nil, err
		}
		if attempt.Status != domain.ForwardingAttemptStatusStarted ||
			!attempt.StartedAt.Before(cutoff) {
			return nil, ports.ErrStoreContractViolation
		}
		if previous != nil &&
			!forwardingRecoveryOrderBefore(*previous, attempt) {
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

func forwardingRecoveryOrderBefore(
	left domain.ForwardingAttempt,
	right domain.ForwardingAttempt,
) bool {
	switch {
	case left.StartedAt.Before(right.StartedAt):
		return true
	case right.StartedAt.Before(left.StartedAt):
		return false
	case left.LocalRequestID < right.LocalRequestID:
		return true
	case left.LocalRequestID > right.LocalRequestID:
		return false
	default:
		return left.AttemptNumber < right.AttemptNumber
	}
}

func lockForwardingAttempt(
	ctx context.Context,
	tx pgx.Tx,
	localRequestID string,
	attemptNumber int,
) error {
	_, err := tx.Exec(
		ctx,
		`
SELECT pg_advisory_xact_lock(
    hashtextextended(
        'tokenio_forwarding_attempt:' || $1 || ':' || ($2::integer)::text,
        0
    )
)
`,
		localRequestID,
		attemptNumber,
	)
	return NormalizeError(err)
}

func forwardingAttemptInsertArgs(
	attempt domain.ForwardingAttempt,
) []any {
	return []any{
		attempt.LocalRequestID,
		attempt.AttemptNumber,
		attempt.RouteID,
		attempt.ResellerID,
		string(attempt.APIFamily),
		string(attempt.EndpointKind),
		attempt.ClientModel,
		string(attempt.ProviderType),
		attempt.ProviderModel,
		string(attempt.Status),
		nullableAttemptState(attempt.AttemptState),
		nullableStatusCode(attempt.UpstreamStatusCode),
		nullableString(attempt.FailureKind),
		attempt.RouteRetryCandidate,
		attempt.StartedAt,
		attempt.CompletedAt,
	}
}

func scanForwardingAttempt(
	row interface{ Scan(...any) error },
) (domain.ForwardingAttempt, error) {
	var attempt domain.ForwardingAttempt
	var apiFamily string
	var endpointKind string
	var providerType string
	var status string
	var attemptState pgtype.Text
	var upstreamStatusCode pgtype.Int4
	var failureKind pgtype.Text
	var completedAt pgtype.Timestamptz

	err := row.Scan(
		&attempt.LocalRequestID,
		&attempt.AttemptNumber,
		&attempt.RouteID,
		&attempt.ResellerID,
		&apiFamily,
		&endpointKind,
		&attempt.ClientModel,
		&providerType,
		&attempt.ProviderModel,
		&status,
		&attemptState,
		&upstreamStatusCode,
		&failureKind,
		&attempt.RouteRetryCandidate,
		&attempt.StartedAt,
		&completedAt,
	)
	if err != nil {
		return domain.ForwardingAttempt{}, NormalizeError(err)
	}

	attempt.APIFamily = domain.APIFamily(apiFamily)
	attempt.EndpointKind = domain.EndpointKind(endpointKind)
	attempt.ProviderType = domain.ProviderType(providerType)
	attempt.Status = domain.ForwardingAttemptStatus(status)
	if attemptState.Valid {
		attempt.AttemptState = domain.ForwardingAttemptState(
			attemptState.String,
		)
	}
	if upstreamStatusCode.Valid {
		attempt.UpstreamStatusCode = int(upstreamStatusCode.Int32)
	}
	if failureKind.Valid {
		attempt.FailureKind = failureKind.String
	}
	if completedAt.Valid {
		value := completedAt.Time
		attempt.CompletedAt = &value
	}

	if err := validatePersistedForwardingAttempt(attempt); err != nil {
		return domain.ForwardingAttempt{}, err
	}
	return attempt, nil
}

func validatePersistedForwardingAttempt(
	attempt domain.ForwardingAttempt,
) error {
	switch attempt.Status {
	case domain.ForwardingAttemptStatusStarted:
		return validateStartedForwardingAttempt(attempt)
	case domain.ForwardingAttemptStatusSucceeded,
		domain.ForwardingAttemptStatusFailed:
		return validateTerminalForwardingAttempt(attempt)
	default:
		return ports.ErrStoreContractViolation
	}
}

func validateStartedForwardingAttempt(
	attempt domain.ForwardingAttempt,
) error {
	if !validForwardingAttemptIdentity(attempt) ||
		attempt.Status != domain.ForwardingAttemptStatusStarted ||
		attempt.AttemptState != "" ||
		attempt.UpstreamStatusCode != 0 ||
		strings.TrimSpace(attempt.FailureKind) != "" ||
		attempt.RouteRetryCandidate ||
		attempt.StartedAt.IsZero() ||
		attempt.StartedAt.Location() != time.UTC ||
		attempt.CompletedAt != nil {
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validateTerminalForwardingAttempt(
	attempt domain.ForwardingAttempt,
) error {
	if !validForwardingAttemptIdentity(attempt) ||
		attempt.StartedAt.IsZero() ||
		attempt.StartedAt.Location() != time.UTC ||
		attempt.CompletedAt == nil ||
		attempt.CompletedAt.IsZero() ||
		attempt.CompletedAt.Location() != time.UTC ||
		attempt.CompletedAt.Before(attempt.StartedAt) {
		return ports.ErrStoreContractViolation
	}

	switch attempt.Status {
	case domain.ForwardingAttemptStatusSucceeded:
		if attempt.AttemptState !=
			domain.ForwardingAttemptStateResponseReceived ||
			attempt.UpstreamStatusCode < 200 ||
			attempt.UpstreamStatusCode > 299 ||
			strings.TrimSpace(attempt.FailureKind) != "" ||
			attempt.RouteRetryCandidate {
			return ports.ErrStoreContractViolation
		}
	case domain.ForwardingAttemptStatusFailed:
		if !validForwardingAttemptState(attempt.AttemptState) ||
			strings.TrimSpace(attempt.FailureKind) == "" ||
			(attempt.UpstreamStatusCode != 0 &&
				(attempt.UpstreamStatusCode < 100 ||
					attempt.UpstreamStatusCode > 599)) {
			return ports.ErrStoreContractViolation
		}
	default:
		return ports.ErrStoreContractViolation
	}
	return nil
}

func validForwardingAttemptIdentity(
	attempt domain.ForwardingAttempt,
) bool {
	return strings.TrimSpace(attempt.LocalRequestID) != "" &&
		attempt.AttemptNumber > 0 &&
		strings.TrimSpace(attempt.RouteID) != "" &&
		strings.TrimSpace(attempt.ResellerID) != "" &&
		attempt.APIFamily != "" &&
		attempt.EndpointKind != "" &&
		strings.TrimSpace(attempt.ClientModel) != "" &&
		attempt.ProviderType != "" &&
		strings.TrimSpace(attempt.ProviderModel) != ""
}

func validForwardingAttemptState(
	state domain.ForwardingAttemptState,
) bool {
	switch state {
	case domain.ForwardingAttemptStateNotSent,
		domain.ForwardingAttemptStateSentNoResponse,
		domain.ForwardingAttemptStateResponseReceived:
		return true
	default:
		return false
	}
}

func sameForwardingAttemptIdentity(
	left domain.ForwardingAttempt,
	right domain.ForwardingAttempt,
) bool {
	return left.LocalRequestID == right.LocalRequestID &&
		left.AttemptNumber == right.AttemptNumber &&
		left.RouteID == right.RouteID &&
		left.ResellerID == right.ResellerID &&
		left.APIFamily == right.APIFamily &&
		left.EndpointKind == right.EndpointKind &&
		left.ClientModel == right.ClientModel &&
		left.ProviderType == right.ProviderType &&
		left.ProviderModel == right.ProviderModel &&
		left.StartedAt.Equal(right.StartedAt)
}

func forwardingAttemptsEqual(
	left domain.ForwardingAttempt,
	right domain.ForwardingAttempt,
) bool {
	return sameForwardingAttemptIdentity(left, right) &&
		left.Status == right.Status &&
		left.AttemptState == right.AttemptState &&
		left.UpstreamStatusCode == right.UpstreamStatusCode &&
		left.FailureKind == right.FailureKind &&
		left.RouteRetryCandidate == right.RouteRetryCandidate &&
		equalTimePointers(left.CompletedAt, right.CompletedAt)
}

func equalTimePointers(left *time.Time, right *time.Time) bool {
	switch {
	case left == nil && right == nil:
		return true
	case left == nil || right == nil:
		return false
	default:
		return left.Equal(*right)
	}
}

func nullableAttemptState(
	value domain.ForwardingAttemptState,
) any {
	if value == "" {
		return nil
	}
	return string(value)
}

func nullableStatusCode(value int) any {
	if value == 0 {
		return nil
	}
	return value
}

func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
