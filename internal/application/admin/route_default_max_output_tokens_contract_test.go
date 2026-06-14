package admin

import (
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestValidateRouteAcceptsEndpointSpecificOutputLimit(
	t *testing.T,
) {
	base := stage10MajorValidAdminRoute()

	tests := []struct {
		name         string
		endpointKind domain.EndpointKind
		capabilities domain.CapabilitySet
		maxOutput    int64
	}{
		{
			name:         "chat requires positive default",
			endpointKind: domain.EndpointChat,
			capabilities: domain.CapabilitySet{
				Chat: true,
			},
			maxOutput: 4096,
		},
		{
			name:         "embeddings requires zero default",
			endpointKind: domain.EndpointEmbeddings,
			capabilities: domain.CapabilitySet{
				Embeddings: true,
			},
			maxOutput: 0,
		},
		{
			name:         "images requires zero default",
			endpointKind: domain.EndpointImagesGeneration,
			capabilities: domain.CapabilitySet{
				ImagesGeneration: true,
			},
			maxOutput: 0,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route := base
			route.EndpointKind = test.endpointKind
			route.Capabilities = test.capabilities
			route.DefaultMaxOutputTokens = test.maxOutput

			if err := validateRoute(route); err != nil {
				t.Fatalf("valid route rejected: %v", err)
			}
		})
	}
}

func TestValidateRouteRejectsEndpointOutputLimitMismatch(
	t *testing.T,
) {
	base := stage10MajorValidAdminRoute()

	tests := []struct {
		name         string
		endpointKind domain.EndpointKind
		capabilities domain.CapabilitySet
		maxOutput    int64
	}{
		{
			name:         "chat zero default",
			endpointKind: domain.EndpointChat,
			capabilities: domain.CapabilitySet{
				Chat: true,
			},
			maxOutput: 0,
		},
		{
			name:         "embeddings nonzero default",
			endpointKind: domain.EndpointEmbeddings,
			capabilities: domain.CapabilitySet{
				Embeddings: true,
			},
			maxOutput: 1,
		},
		{
			name:         "images nonzero default",
			endpointKind: domain.EndpointImagesGeneration,
			capabilities: domain.CapabilitySet{
				ImagesGeneration: true,
			},
			maxOutput: 1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route := base
			route.EndpointKind = test.endpointKind
			route.Capabilities = test.capabilities
			route.DefaultMaxOutputTokens = test.maxOutput

			if err := validateRoute(route); !errors.Is(
				err,
				ErrInvalidRequest,
			) {
				t.Fatalf(
					"error=%v want=%v route=%+v",
					err,
					ErrInvalidRequest,
					route,
				)
			}
		})
	}
}
