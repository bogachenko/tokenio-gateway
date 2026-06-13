package rewritesupport

import (
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

var ErrInvalidRegistry = errors.New(
	"invalid model rewrite support registry",
)

type Registry struct {
	supports []ports.ModelIdentifierRewriteSupport
}

var _ ports.ModelIdentifierRewriteSupport = (*Registry)(nil)

func NewRegistry(
	supports ...ports.ModelIdentifierRewriteSupport,
) (*Registry, error) {
	if len(supports) == 0 {
		return nil, ErrInvalidRegistry
	}

	values := make(
		[]ports.ModelIdentifierRewriteSupport,
		len(supports),
	)
	for index, support := range supports {
		if support == nil {
			return nil, ErrInvalidRegistry
		}
		values[index] = support
	}
	return &Registry{supports: values}, nil
}

func (r *Registry) SupportsModelIdentifierRewrite(
	apiFamily domain.APIFamily,
	providerType domain.ProviderType,
) bool {
	if r == nil {
		return false
	}
	for _, support := range r.supports {
		if support.SupportsModelIdentifierRewrite(
			apiFamily,
			providerType,
		) {
			return true
		}
	}
	return false
}
