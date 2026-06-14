package admin

import (
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestValidateRouteAcceptsCanonicalAncillaryCapabilities(
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
			name:         "chat with independent optional capabilities",
			endpointKind: domain.EndpointChat,
			capabilities: domain.CapabilitySet{
				Chat:           true,
				Tools:          true,
				ToolChoice:     true,
				ResponseFormat: true,
				JSONSchema:     true,
				ImageInput:     true,
				AudioInput:     true,
				FileInput:      true,
				VideoInput:     true,
				Reasoning:      true,
			},
			maxOutput: 4096,
		},
		{
			name:         "chat response format without schema",
			endpointKind: domain.EndpointChat,
			capabilities: domain.CapabilitySet{
				Chat:           true,
				ResponseFormat: true,
			},
			maxOutput: 4096,
		},
		{
			name:         "chat tools without tool choice",
			endpointKind: domain.EndpointChat,
			capabilities: domain.CapabilitySet{
				Chat:  true,
				Tools: true,
			},
			maxOutput: 4096,
		},
		{
			name:         "embeddings has no ancillary capabilities",
			endpointKind: domain.EndpointEmbeddings,
			capabilities: domain.CapabilitySet{
				Embeddings: true,
			},
		},
		{
			name:         "images has no ancillary capabilities",
			endpointKind: domain.EndpointImagesGeneration,
			capabilities: domain.CapabilitySet{
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

			if err := validateRoute(route); err != nil {
				t.Fatalf("valid route rejected: %v", err)
			}
		})
	}
}

func TestValidateRouteRejectsInvalidAncillaryCapabilities(
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
			name:         "tool choice without tools",
			endpointKind: domain.EndpointChat,
			capabilities: domain.CapabilitySet{
				Chat:       true,
				ToolChoice: true,
			},
			maxOutput: 4096,
		},
		{
			name:         "json schema without response format",
			endpointKind: domain.EndpointChat,
			capabilities: domain.CapabilitySet{
				Chat:       true,
				JSONSchema: true,
			},
			maxOutput: 4096,
		},
		{
			name:         "embeddings with tools",
			endpointKind: domain.EndpointEmbeddings,
			capabilities: domain.CapabilitySet{
				Embeddings: true,
				Tools:      true,
			},
		},
		{
			name:         "embeddings with response format",
			endpointKind: domain.EndpointEmbeddings,
			capabilities: domain.CapabilitySet{
				Embeddings:     true,
				ResponseFormat: true,
			},
		},
		{
			name:         "embeddings with media input",
			endpointKind: domain.EndpointEmbeddings,
			capabilities: domain.CapabilitySet{
				Embeddings: true,
				ImageInput: true,
			},
		},
		{
			name:         "embeddings with reasoning",
			endpointKind: domain.EndpointEmbeddings,
			capabilities: domain.CapabilitySet{
				Embeddings: true,
				Reasoning:  true,
			},
		},
		{
			name:         "images with tools",
			endpointKind: domain.EndpointImagesGeneration,
			capabilities: domain.CapabilitySet{
				ImagesGeneration: true,
				Tools:            true,
			},
		},
		{
			name:         "images with json schema",
			endpointKind: domain.EndpointImagesGeneration,
			capabilities: domain.CapabilitySet{
				ImagesGeneration: true,
				ResponseFormat:   true,
				JSONSchema:       true,
			},
		},
		{
			name:         "images with media input",
			endpointKind: domain.EndpointImagesGeneration,
			capabilities: domain.CapabilitySet{
				ImagesGeneration: true,
				AudioInput:       true,
			},
		},
		{
			name:         "images with reasoning",
			endpointKind: domain.EndpointImagesGeneration,
			capabilities: domain.CapabilitySet{
				ImagesGeneration: true,
				Reasoning:        true,
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
