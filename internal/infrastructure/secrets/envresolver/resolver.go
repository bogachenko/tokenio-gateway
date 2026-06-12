package envresolver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var _ ports.SecretResolver = (*Resolver)(nil)

var (
	ErrInvalidSecretName  = errors.New("invalid secret name")
	ErrSecretNotAvailable = errors.New("secret not available")
)

type Resolver struct {
	lookup func(string) (string, bool)
}

func New() *Resolver {
	return &Resolver{lookup: os.LookupEnv}
}

func NewWithLookup(lookup func(string) (string, bool)) (*Resolver, error) {
	if lookup == nil {
		return nil, fmt.Errorf("%w", ErrInvalidSecretName)
	}
	return &Resolver{lookup: lookup}, nil
}

func (r *Resolver) Resolve(ctx context.Context, name string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("%w", ErrInvalidSecretName)
	}
	lookup := os.LookupEnv
	if r != nil && r.lookup != nil {
		lookup = r.lookup
	}
	value, ok := lookup(name)
	if !ok || value == "" {
		return "", fmt.Errorf("%w", ErrSecretNotAvailable)
	}
	return value, nil
}
