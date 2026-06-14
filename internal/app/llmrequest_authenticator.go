package app

import (
	"context"
	"fmt"

	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	"github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
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

var _ llmrequest.Authenticator = (*LLMRequestAuthenticator)(nil)

func NewLLMRequestAuthenticator(
	public publicRequestAuthenticator,
) (*LLMRequestAuthenticator, error) {
	if public == nil {
		return nil, llmrequest.ErrDependencyRequired
	}
	return &LLMRequestAuthenticator{public: public}, nil
}

func (a *LLMRequestAuthenticator) Authenticate(
	ctx context.Context,
	rawAPIKey string,
) (llmrequest.Principal, error) {
	if a == nil || a.public == nil {
		return llmrequest.Principal{}, llmrequest.ErrDependencyRequired
	}
	if ctx == nil {
		return llmrequest.Principal{}, fmt.Errorf(
			"%w: nil authentication context",
			llmrequest.ErrInvalidInput,
		)
	}
	if err := ctx.Err(); err != nil {
		return llmrequest.Principal{}, err
	}

	result, err := a.public.AuthenticatePublicRequest(
		ctx,
		authenticateapp.Input{RawAPIKey: rawAPIKey},
	)
	if err != nil {
		return llmrequest.Principal{}, fmt.Errorf(
			"authenticate public LLM request: %w",
			err,
		)
	}

	return llmrequest.Principal{
		UserID:   result.Principal.UserID,
		APIKeyID: result.Principal.APIKeyID,
		BillingSubjectUserID: result.Principal.
			BillingSubjectUserID,
	}, nil
}
