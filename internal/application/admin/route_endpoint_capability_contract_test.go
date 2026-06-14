package admin

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestValidateRouteAcceptsExactEndpointCapability(
	t *testing.T,
) {
	at := time.Unix(1, 0).UTC()

	tests := []struct {
		name         string
		endpointKind domain.EndpointKind
		capabilities domain.CapabilitySet
		maxOutput    int64
	}{
		{
			name:         "chat",
			endpointKind: domain.EndpointChat,
			capabilities: domain.CapabilitySet{
				Chat:           true,
				Tools:          true,
				ImageInput:     true,
				ResponseFormat: true,
			},
			maxOutput: 4096,
		},
		{
			name:         "embeddings",
			endpointKind: domain.EndpointEmbeddings,
			capabilities: domain.CapabilitySet{
				Embeddings: true,
			},
		},
		{
			name:         "images generation",
			endpointKind: domain.EndpointImagesGeneration,
			capabilities: domain.CapabilitySet{
				ImagesGeneration: true,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			route := domain.Route{
				ID:                     "route_1",
				ResellerID:             "reseller_1",
				ProviderType:           domain.ProviderOpenAI,
				APIFamily:              domain.APIFamilyOpenAICompatible,
				EndpointKind:           test.endpointKind,
				ClientModel:            "model-1",
				ProviderModel:          "model-1",
				ModelRewritePolicy:     domain.ModelRewritePolicyNone,
				Enabled:                true,
				Priority:               100,
				DefaultMaxOutputTokens: test.maxOutput,
				Capabilities:           test.capabilities,
				CreatedAt:              at,
				UpdatedAt:              at,
			}

			if err := validateRoute(route); err != nil {
				t.Fatalf("valid route rejected: %v", err)
			}
		})
	}
}

func TestValidateRouteRejectsEndpointCapabilityMismatch(
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
			name:         "chat capability missing",
			endpointKind: domain.EndpointChat,
			capabilities: domain.CapabilitySet{},
			maxOutput:    4096,
		},
		{
			name:         "chat declares embeddings",
			endpointKind: domain.EndpointChat,
			capabilities: domain.CapabilitySet{
				Chat:       true,
				Embeddings: true,
			},
			maxOutput: 4096,
		},
		{
			name:         "chat declares images generation",
			endpointKind: domain.EndpointChat,
			capabilities: domain.CapabilitySet{
				Chat:             true,
				ImagesGeneration: true,
			},
			maxOutput: 4096,
		},
		{
			name:         "embeddings capability missing",
			endpointKind: domain.EndpointEmbeddings,
			capabilities: domain.CapabilitySet{},
		},
		{
			name:         "embeddings declares chat",
			endpointKind: domain.EndpointEmbeddings,
			capabilities: domain.CapabilitySet{
				Chat:       true,
				Embeddings: true,
			},
		},
		{
			name:         "embeddings declares images generation",
			endpointKind: domain.EndpointEmbeddings,
			capabilities: domain.CapabilitySet{
				Embeddings:       true,
				ImagesGeneration: true,
			},
		},
		{
			name:         "images capability missing",
			endpointKind: domain.EndpointImagesGeneration,
			capabilities: domain.CapabilitySet{},
		},
		{
			name:         "images declares chat",
			endpointKind: domain.EndpointImagesGeneration,
			capabilities: domain.CapabilitySet{
				Chat:             true,
				ImagesGeneration: true,
			},
		},
		{
			name:         "images declares embeddings",
			endpointKind: domain.EndpointImagesGeneration,
			capabilities: domain.CapabilitySet{
				Embeddings:       true,
				ImagesGeneration: true,
			},
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
