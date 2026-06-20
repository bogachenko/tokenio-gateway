package llmrequest

import (
	"context"
	"fmt"
)

type publicRequestAuthenticator interface {
	AuthenticatePublicRequest(context.Context, string) (Principal, error)
}

type LLMRequestAuthenticator struct {
	public publicRequestAuthenticator
}

var _ Authenticator = (*LLMRequestAuthenticator)(nil)

func NewLLMRequestAuthenticator(
	public publicRequestAuthenticator,
) (*LLMRequestAuthenticator, error) {
	if public == nil {
		return nil, ErrDependencyRequired
	}
	return &LLMRequestAuthenticator{public: public}, nil
}

func (a *LLMRequestAuthenticator) Authenticate(
	ctx context.Context,
	rawAPIKey string,
) (Principal, error) {
	if a == nil || a.public == nil {
		return Principal{}, ErrDependencyRequired
	}
	if ctx == nil {
		return Principal{}, fmt.Errorf(
			"%w: nil authentication context",
			ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return Principal{}, err
	}

	principal, err := a.public.AuthenticatePublicRequest(ctx, rawAPIKey)
	if err != nil {
		return Principal{}, fmt.Errorf(
			"authenticate public LLM request: %w",
			err,
		)
	}

	return principal, nil
}
