package postgres

import (
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestAdminRouteRepositoryConstructorsRejectNilDB(t *testing.T) {
	constructors := []func() error{
		func() error {
			_, err := NewAdminRouteRepository(nil)
			return err
		},
		func() error {
			_, err := NewAdminRoutePriceRepository(nil)
			return err
		},
	}
	for index, constructor := range constructors {
		if err := constructor(); !errors.Is(
			err,
			ErrInvalidDatabaseConfig,
		) {
			t.Fatalf(
				"constructor %d error = %v, want invalid config",
				index,
				err,
			)
		}
	}
}

func TestValidateAdminRouteMutationShapes(t *testing.T) {
	base := adminRouteTestRecord()

	tests := []struct {
		name   string
		action domain.AuditAction
		next   func(domain.Route) domain.Route
	}{
		{
			name:   "update",
			action: domain.AuditActionRouteUpdate,
			next: func(value domain.Route) domain.Route {
				value.ProviderModel = "provider-model-2"
				value.Priority++
				value.UpdatedAt = value.UpdatedAt.Add(time.Second)
				return value
			},
		},
		{
			name:   "disable",
			action: domain.AuditActionRouteDisable,
			next: func(value domain.Route) domain.Route {
				at := value.UpdatedAt.Add(time.Second)
				value.Enabled = false
				value.DisabledAt = &at
				value.UpdatedAt = at
				return value
			},
		},
		{
			name:   "cooldown set",
			action: domain.AuditActionRouteCooldownSet,
			next: func(value domain.Route) domain.Route {
				at := value.UpdatedAt.Add(time.Second)
				until := at.Add(time.Hour)
				value.CooldownUntil = &until
				value.CooldownReason = "rate_limited"
				value.UpdatedAt = at
				return value
			},
		},
		{
			name:   "cooldown clear",
			action: domain.AuditActionRouteCooldownClear,
			next: func(value domain.Route) domain.Route {
				at := value.UpdatedAt.Add(time.Second)
				until := at.Add(time.Hour)
				value.CooldownUntil = &until
				value.CooldownReason = "rate_limited"
				value.UpdatedAt = at

				nextAt := at.Add(time.Second)
				value.CooldownUntil = nil
				value.CooldownReason = ""
				value.UpdatedAt = nextAt
				return value
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			expected := base
			if test.action == domain.AuditActionRouteCooldownClear {
				at := expected.UpdatedAt.Add(time.Second)
				until := at.Add(time.Hour)
				expected.CooldownUntil = &until
				expected.CooldownReason = "rate_limited"
				expected.UpdatedAt = at
			}
			next := test.next(expected)
			if err := validateAdminRouteMutation(
				expected,
				next,
				test.action,
			); err != nil {
				t.Fatalf("validation error: %v", err)
			}
		})
	}
}

func TestValidateAdminRouteMutationRejectsProviderChange(
	t *testing.T,
) {
	base := adminRouteTestRecord()
	next := base
	next.ProviderType = domain.ProviderGroq
	next.UpdatedAt = next.UpdatedAt.Add(time.Second)

	if err := validateAdminRouteMutation(
		base,
		next,
		domain.AuditActionRouteUpdate,
	); !errors.Is(err, ports.ErrStoreContractViolation) {
		t.Fatalf("error = %v, want contract violation", err)
	}
}

func TestAdminRoutePriceValidationRejectsInvalidNumbers(
	t *testing.T,
) {
	value := adminRoutePriceTestRecord()
	value.InputPricePer1MTokensCents = -1
	if err := validateAdminRoutePriceRecord(value); !errors.Is(
		err,
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf("error = %v, want contract violation", err)
	}
}

func adminRouteTestRecord() domain.Route {
	now := time.Unix(100, 123456000).UTC()
	return domain.Route{
		ID:                     "route-1",
		ResellerID:             "reseller-1",
		ProviderType:           domain.ProviderOpenAI,
		APIFamily:              domain.APIFamilyOpenAICompatible,
		EndpointKind:           domain.EndpointChat,
		ClientModel:            "client-model",
		ProviderModel:          "provider-model",
		ModelRewritePolicy:     domain.ModelRewritePolicyNone,
		Enabled:                true,
		Priority:               10,
		RequestsPerMinute:      100,
		TokensPerMinute:        1000,
		ConcurrentRequests:     5,
		DefaultMaxOutputTokens: 4096,
		Capabilities:           domain.CapabilitySet{Chat: true},
		CreatedAt:              now,
		UpdatedAt:              now,
	}
}

func adminRoutePriceTestRecord() domain.RoutePrice {
	now := time.Unix(100, 123456000).UTC()
	return domain.RoutePrice{
		RouteID:                     "route-1",
		Currency:                    "RUB",
		InputPricePer1MTokensCents:  10,
		OutputPricePer1MTokensCents: 20,
		ImageGenerationUnitKind:     domain.ImageGenerationUnitKindNone,
		MarkupCoefficient:           1.25,
		Enabled:                     true,
		CreatedAt:                   now,
		UpdatedAt:                   now,
	}
}
