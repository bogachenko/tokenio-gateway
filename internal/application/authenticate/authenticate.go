package authenticate

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var (
	ErrInvalidAPIKey = &ports.ApplicationError{
		Code:         domain.ErrorCodeInvalidAPIKey,
		SafeMessage:  "Invalid API key",
		Category:     ports.FailureCategoryUnauthorized,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("invalid api key"),
	}
	ErrUserDisabled = &ports.ApplicationError{
		Code:         domain.ErrorCodeUserDisabled,
		SafeMessage:  "User is disabled",
		Category:     ports.FailureCategoryForbidden,
		Retryability: ports.RetryabilityNonRetryable,
		RequestStage: ports.RequestStagePreForwarding,
		Cause:        errors.New("user disabled"),
	}
	ErrInvalidIdentity = errors.New("invalid identity")
)

type Input struct {
	RawAPIKey string
}

type Result struct {
	Principal auth.APIKeyPrincipal
}

type UseCase struct {
	hasher *auth.APIKeyHasher
	keys   ports.APIKeyRepository
	users  ports.UserRepository
	clock  ports.Clock
}

func NewUseCase(
	hasher *auth.APIKeyHasher,
	keys ports.APIKeyRepository,
	users ports.UserRepository,
	clock ports.Clock,
) (*UseCase, error) {
	if hasher == nil {
		return nil, errors.New("api key hasher is required")
	}
	if keys == nil {
		return nil, errors.New("api key repository is required")
	}
	if users == nil {
		return nil, errors.New("user repository is required")
	}
	if clock == nil {
		return nil, errors.New("clock is required")
	}

	return &UseCase{
		hasher: hasher,
		keys:   keys,
		users:  users,
		clock:  clock,
	}, nil
}

func (u *UseCase) AuthenticatePublicRequest(ctx context.Context, input Input) (Result, error) {
	if strings.TrimSpace(input.RawAPIKey) == "" {
		return Result{}, ErrInvalidAPIKey
	}

	keyHash := u.hasher.Hash(input.RawAPIKey)
	key, err := u.keys.FindByHash(ctx, keyHash)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return Result{}, ErrInvalidAPIKey
		}
		return Result{}, fmt.Errorf("find api key by hash: %w", err)
	}
	if key == nil || !key.Enabled || key.RevokedAt != nil {
		return Result{}, ErrInvalidAPIKey
	}
	if key.ExpiresAt != nil && !key.ExpiresAt.After(u.clock.Now()) {
		return Result{}, ErrInvalidAPIKey
	}

	user, err := u.users.FindByID(ctx, key.UserID)
	if err != nil {
		if errors.Is(err, ports.ErrNotFound) {
			return Result{}, ErrInvalidAPIKey
		}
		return Result{}, fmt.Errorf("find user by id: %w", err)
	}
	if user == nil {
		return Result{}, ErrInvalidAPIKey
	}
	if !user.Enabled {
		return Result{}, ErrUserDisabled
	}
	if strings.TrimSpace(user.ExternalBillingUserID) == "" {
		return Result{}, ErrInvalidIdentity
	}

	return Result{
		Principal: auth.APIKeyPrincipal{
			UserID:               user.ID,
			APIKeyID:             key.ID,
			BillingSubjectUserID: user.ExternalBillingUserID,
		},
	}, nil
}
