package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type endpointPriceContractRoutes struct {
	ports.AdminRouteRepository
	route domain.Route
}

func (s *endpointPriceContractRoutes) FindRouteByID(
	context.Context,
	string,
) (*domain.Route, error) {
	copied := s.route
	return &copied, nil
}

type endpointPriceContractPrices struct {
	ports.AdminRoutePriceRepository
	upsertCalls int
}

func (s *endpointPriceContractPrices) FindRoutePrice(
	context.Context,
	string,
) (*domain.RoutePrice, error) {
	return nil, ports.ErrNotFound
}

func (s *endpointPriceContractPrices) UpsertRoutePriceWithAudit(
	_ context.Context,
	_ *domain.RoutePrice,
	next domain.RoutePrice,
	_ domain.AuditContext,
) (domain.RoutePrice, error) {
	s.upsertCalls++
	return next, nil
}

type endpointPriceContractValidator struct{}

func (endpointPriceContractValidator) ValidateRoutePrice(
	domain.RoutePrice,
) error {
	return nil
}

func TestValidateRoutePriceForEndpointAcceptsCanonicalDimensions(
	t *testing.T,
) {
	tests := []struct {
		name  string
		route domain.Route
		price domain.RoutePrice
	}{
		{
			name: "chat token and media dimensions",
			route: domain.Route{
				ID:           "route_chat",
				EndpointKind: domain.EndpointChat,
			},
			price: domain.RoutePrice{
				RouteID:                         "route_chat",
				InputPricePer1MTokensCents:      10,
				OutputPricePer1MTokensCents:     20,
				ImageInputPricePer1MTokensCents: 30,
				ImageGenerationUnitKind: domain.
					ImageGenerationUnitKindNone,
			},
		},
		{
			name: "embeddings input dimension only",
			route: domain.Route{
				ID:           "route_embeddings",
				EndpointKind: domain.EndpointEmbeddings,
			},
			price: domain.RoutePrice{
				RouteID:                    "route_embeddings",
				InputPricePer1MTokensCents: 10,
				ImageGenerationUnitKind: domain.
					ImageGenerationUnitKindNone,
			},
		},
		{
			name: "images generated image dimension only",
			route: domain.Route{
				ID: "route_images",
				EndpointKind: domain.
					EndpointImagesGeneration,
			},
			price: domain.RoutePrice{
				RouteID:                          "route_images",
				ImageGenerationPricePerUnitCents: 10,
				ImageGenerationUnitKind: domain.
					ImageGenerationUnitKindGeneratedImage,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateRoutePriceForEndpoint(
				test.route,
				test.price,
			); err != nil {
				t.Fatalf("validation error: %v", err)
			}
		})
	}
}

func TestValidateRoutePriceForEndpointRejectsUnbillableDimensions(
	t *testing.T,
) {
	tests := []struct {
		name  string
		route domain.Route
		price domain.RoutePrice
	}{
		{
			name: "route id mismatch",
			route: domain.Route{
				ID:           "route_chat",
				EndpointKind: domain.EndpointChat,
			},
			price: domain.RoutePrice{
				RouteID: "other",
				ImageGenerationUnitKind: domain.
					ImageGenerationUnitKindNone,
			},
		},
		{
			name: "chat with image generation dimension",
			route: domain.Route{
				ID:           "route_chat",
				EndpointKind: domain.EndpointChat,
			},
			price: domain.RoutePrice{
				RouteID:                          "route_chat",
				ImageGenerationPricePerUnitCents: 1,
				ImageGenerationUnitKind: domain.
					ImageGenerationUnitKindGeneratedImage,
			},
		},
		{
			name: "embeddings with output token dimension",
			route: domain.Route{
				ID:           "route_embeddings",
				EndpointKind: domain.EndpointEmbeddings,
			},
			price: domain.RoutePrice{
				RouteID:                     "route_embeddings",
				InputPricePer1MTokensCents:  1,
				OutputPricePer1MTokensCents: 1,
				ImageGenerationUnitKind: domain.
					ImageGenerationUnitKindNone,
			},
		},
		{
			name: "embeddings with media dimension",
			route: domain.Route{
				ID:           "route_embeddings",
				EndpointKind: domain.EndpointEmbeddings,
			},
			price: domain.RoutePrice{
				RouteID:                         "route_embeddings",
				InputPricePer1MTokensCents:      1,
				ImageInputPricePer1MTokensCents: 1,
				ImageGenerationUnitKind: domain.
					ImageGenerationUnitKindNone,
			},
		},
		{
			name: "images with token dimension",
			route: domain.Route{
				ID: "route_images",
				EndpointKind: domain.
					EndpointImagesGeneration,
			},
			price: domain.RoutePrice{
				RouteID:                          "route_images",
				InputPricePer1MTokensCents:       1,
				ImageGenerationPricePerUnitCents: 1,
				ImageGenerationUnitKind: domain.
					ImageGenerationUnitKindGeneratedImage,
			},
		},
		{
			name: "images without generated image unit kind",
			route: domain.Route{
				ID: "route_images",
				EndpointKind: domain.
					EndpointImagesGeneration,
			},
			price: domain.RoutePrice{
				RouteID:                          "route_images",
				ImageGenerationPricePerUnitCents: 1,
				ImageGenerationUnitKind: domain.
					ImageGenerationUnitKindNone,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if err := validateRoutePriceForEndpoint(
				test.route,
				test.price,
			); !errors.Is(err, ErrInvalidRequest) {
				t.Fatalf(
					"error=%v want=%v",
					err,
					ErrInvalidRequest,
				)
			}
		})
	}
}

func TestUpsertRoutePriceRejectsEndpointMismatchBeforeMutation(
	t *testing.T,
) {
	routes := &endpointPriceContractRoutes{
		route: domain.Route{
			ID: "route_images",
			EndpointKind: domain.
				EndpointImagesGeneration,
		},
	}
	prices := &endpointPriceContractPrices{}
	service := &Service{
		deps: Dependencies{
			Routes:         routes,
			Prices:         prices,
			PriceValidator: endpointPriceContractValidator{},
			Clock: fixedClock{
				value: time.Unix(100, 0).UTC(),
			},
		},
	}

	_, err := service.UpsertRoutePrice(
		context.Background(),
		command(),
		domain.RoutePrice{
			RouteID:                          "route_images",
			Currency:                         "RUB",
			InputPricePer1MTokensCents:       1,
			ImageGenerationPricePerUnitCents: 10,
			ImageGenerationUnitKind: domain.
				ImageGenerationUnitKindGeneratedImage,
			MarkupCoefficient: 1,
			Enabled:           true,
		},
	)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error=%v want=%v", err, ErrInvalidRequest)
	}
	if prices.upsertCalls != 0 {
		t.Fatalf("upsert calls=%d", prices.upsertCalls)
	}
}
