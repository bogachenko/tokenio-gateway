package llmrequest

import (
	"context"
	"fmt"

	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
)

type publicRequestAuthenticator interface {
	AuthenticatePublicRequest(
		context.Context,
		authenticateapp.Input,
	) (authenticateapp.Result, error)
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

	result, err := a.public.AuthenticatePublicRequest(
		ctx,
		authenticateapp.Input{RawAPIKey: rawAPIKey},
	)
	if err != nil {
		return Principal{}, fmt.Errorf(
			"authenticate public LLM request: %w",
			err,
		)
	}

	return Principal{
		UserID:               result.Principal.UserID,
		APIKeyID:             result.Principal.APIKeyID,
		BillingSubjectUserID: result.Principal.BillingSubjectUserID,
	}, nil
}
