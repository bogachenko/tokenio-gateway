package llmrequest

import (
	"context"
	"errors"
	"testing"
)

type publicRequestAuthenticatorFunc func(
	context.Context,
	string,
) (Principal, error)

func (function publicRequestAuthenticatorFunc) AuthenticatePublicRequest(
	ctx context.Context,
	rawAPIKey string,
) (Principal, error) {
	return function(ctx, rawAPIKey)
}

func TestLLMRequestAuthenticatorMapsPublicPrincipal(t *testing.T) {
	var gotRawAPIKey string
	adapter, err := NewLLMRequestAuthenticator(
		publicRequestAuthenticatorFunc(
			func(
				_ context.Context,
				rawAPIKey string,
			) (Principal, error) {
				gotRawAPIKey = rawAPIKey
				return Principal{
					UserID:               "user-1",
					APIKeyID:             "key-1",
					BillingSubjectUserID: "billing-1",
				}, nil
			},
		),
	)
	if err != nil {
		t.Fatalf("NewLLMRequestAuthenticator: %v", err)
	}

	principal, err := adapter.Authenticate(
		context.Background(),
		"  exact-api-key  ",
	)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if gotRawAPIKey != "  exact-api-key  " {
		t.Fatalf("raw API key = %q", gotRawAPIKey)
	}
	if principal != (Principal{
		UserID:               "user-1",
		APIKeyID:             "key-1",
		BillingSubjectUserID: "billing-1",
	}) {
		t.Fatalf("principal = %+v", principal)
	}
}

func TestLLMRequestAuthenticatorPreservesAuthenticationError(t *testing.T) {
	stageError := errors.New("authentication failed")
	adapter, err := NewLLMRequestAuthenticator(
		publicRequestAuthenticatorFunc(
			func(
				context.Context,
				string,
			) (Principal, error) {
				return Principal{}, stageError
			},
		),
	)
	if err != nil {
		t.Fatalf("NewLLMRequestAuthenticator: %v", err)
	}

	_, err = adapter.Authenticate(context.Background(), "api-key")
	if !errors.Is(err, stageError) {
		t.Fatalf("error = %v", err)
	}
}

func TestLLMRequestAuthenticatorRejectsNilContextBeforeDependency(t *testing.T) {
	called := false
	adapter, err := NewLLMRequestAuthenticator(
		publicRequestAuthenticatorFunc(
			func(context.Context, string) (Principal, error) {
				called = true
				return Principal{}, nil
			},
		),
	)
	if err != nil {
		t.Fatalf("NewLLMRequestAuthenticator: %v", err)
	}

	_, err = adapter.Authenticate(nil, "api-key")
	if !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("error = %v", err)
	}
	if called {
		t.Fatal("dependency was called")
	}
}

func TestLLMRequestAuthenticatorPropagatesCanceledContext(t *testing.T) {
	called := false
	adapter, err := NewLLMRequestAuthenticator(
		publicRequestAuthenticatorFunc(
			func(context.Context, string) (Principal, error) {
				called = true
				return Principal{}, nil
			},
		),
	)
	if err != nil {
		t.Fatalf("NewLLMRequestAuthenticator: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err = adapter.Authenticate(ctx, "api-key")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
	if called {
		t.Fatal("dependency was called")
	}
}

func TestNewLLMRequestAuthenticatorRequiresDependency(t *testing.T) {
	_, err := NewLLMRequestAuthenticator(nil)
	if !errors.Is(err, ErrDependencyRequired) {
		t.Fatalf("error = %v", err)
	}
}
