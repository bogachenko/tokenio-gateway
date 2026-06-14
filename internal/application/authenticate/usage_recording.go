package authenticate

import (
	"context"
	"errors"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type PublicAuthenticator interface {
	AuthenticatePublicRequest(
		context.Context,
		Input,
	) (Result, error)
}

type UsageRecordingAuthenticator struct {
	next     PublicAuthenticator
	recorder ports.APIKeyUsageRecorder
	clock    ports.Clock
	timeout  time.Duration
}

var _ PublicAuthenticator = (*UseCase)(nil)
var _ PublicAuthenticator = (*UsageRecordingAuthenticator)(nil)

func NewUsageRecordingAuthenticator(
	next PublicAuthenticator,
	recorder ports.APIKeyUsageRecorder,
	clock ports.Clock,
	timeout time.Duration,
) (*UsageRecordingAuthenticator, error) {
	switch {
	case next == nil:
		return nil, errors.New(
			"public authenticator is required",
		)
	case recorder == nil:
		return nil, errors.New(
			"api key usage recorder is required",
		)
	case clock == nil:
		return nil, errors.New(
			"clock is required",
		)
	case timeout <= 0:
		return nil, errors.New(
			"api key last-used timeout must be positive",
		)
	}

	return &UsageRecordingAuthenticator{
		next:     next,
		recorder: recorder,
		clock:    clock,
		timeout:  timeout,
	}, nil
}

func (a *UsageRecordingAuthenticator) AuthenticatePublicRequest(
	ctx context.Context,
	input Input,
) (Result, error) {
	result, err := a.next.AuthenticatePublicRequest(
		ctx,
		input,
	)
	if err != nil {
		return Result{}, err
	}

	baseContext := context.Background()
	if ctx != nil {
		baseContext = context.WithoutCancel(ctx)
	}
	recordContext, cancel := context.WithTimeout(
		baseContext,
		a.timeout,
	)
	defer cancel()

	usedAt := a.clock.Now().UTC()
	_ = a.recorder.RecordLastUsedAt(
		recordContext,
		result.Principal.APIKeyID,
		usedAt,
	)

	return result, nil
}
