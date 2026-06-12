package routing

import (
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestSelectionUsesExactRoutingKey(t *testing.T) {
	now := fixedNow()
	exact := baseCandidate("route-exact")
	otherFamily := baseCandidate("route-other-family")
	otherFamily.Route.APIFamily = domain.APIFamily("other_family")
	otherKind := baseCandidate("route-other-kind")
	otherKind.Route.EndpointKind = domain.EndpointKind("other_kind")
	otherModel := baseCandidate("route-other-model")
	otherModel.Route.ClientModel = "other-model"
	otherModel.Route.ProviderModel = "other-model"

	result, err := Select(baseInput(now, domain.CapabilitySet{}, []Candidate{otherFamily, otherKind, otherModel, exact}))
	if err != nil {
		t.Fatalf("Select returned error: %v", err)
	}
	if result.Selected.Route.ID != "route-exact" {
		t.Fatalf("selected = %q, want route-exact", result.Selected.Route.ID)
	}
	if len(result.Fallbacks) != 0 {
		t.Fatalf("fallbacks = %v, want none", routeIDs(result.Fallbacks))
	}
}

func TestSelectionInputValidationAndUnknownModel(t *testing.T) {
	now := fixedNow()
	tests := []struct {
		name  string
		input SelectionInput
		want  error
	}{
		{name: "no exact key candidate", input: baseInput(now, domain.CapabilitySet{}, []Candidate{withClientModel(baseCandidate("route-other"), "other-model")}), want: ErrUnknownModel},
		{name: "empty API family", input: SelectionInput{Query: ports.RouteQuery{EndpointKind: domain.EndpointKind("kind"), ClientModel: "model"}, Now: now}, want: ErrInvalidSelectionInput},
		{name: "empty endpoint kind", input: SelectionInput{Query: ports.RouteQuery{APIFamily: domain.APIFamily("family"), ClientModel: "model"}, Now: now}, want: ErrInvalidSelectionInput},
		{name: "blank client model", input: SelectionInput{Query: ports.RouteQuery{APIFamily: domain.APIFamily("family"), EndpointKind: domain.EndpointKind("kind"), ClientModel: " \t\n "}, Now: now}, want: ErrInvalidSelectionInput},
		{name: "zero now", input: SelectionInput{Query: ports.RouteQuery{APIFamily: domain.APIFamily("family"), EndpointKind: domain.EndpointKind("kind"), ClientModel: "model"}}, want: ErrInvalidSelectionInput},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Select(tt.input)
			if !errors.Is(err, tt.want) {
				t.Fatalf("error = %v, want %v", err, tt.want)
			}
		})
	}
}

func TestOperationalAvailabilitySkipReasons(t *testing.T) {
	now := fixedNow()
	future := now.Add(time.Minute)
	past := now.Add(-time.Minute)

	tests := []struct {
		name       string
		mutate     func(*Candidate)
		wantReason SkipReason
	}{
		{name: "disabled route", mutate: func(c *Candidate) { c.Route.Enabled = false }, wantReason: SkipReasonManualDisabled},
		{name: "disabled reseller", mutate: func(c *Candidate) { c.Reseller.Enabled = false }, wantReason: SkipReasonManualDisabled},
		{name: "missing secret", mutate: func(c *Candidate) { c.SecretAvailable = false }, wantReason: SkipReasonMissingResellerAPIKey},
		{name: "active cooldown", mutate: func(c *Candidate) { c.Route.CooldownUntil = &future }, wantReason: SkipReasonCooldownActive},
		{name: "missing cost", mutate: func(c *Candidate) { c.CostAvailable = false }, wantReason: SkipReasonPricingUnavailable},
		{name: "negative estimated cost", mutate: func(c *Candidate) { c.EstimatedUpstreamCostCents = -1 }, wantReason: SkipReasonInvalidRoutePrice},
		{name: "insufficient reseller balance", mutate: func(c *Candidate) { c.EstimatedUpstreamCostCents = 101 }, wantReason: SkipReasonInsufficientResellerBalance},
		{name: "reserved cents included", mutate: func(c *Candidate) { c.Reseller.ReservedCents = 51 }, wantReason: SkipReasonInsufficientResellerBalance},
		{name: "minimum balance cents included", mutate: func(c *Candidate) { c.Reseller.MinimumBalanceCents = 51 }, wantReason: SkipReasonInsufficientResellerBalance},
		{name: "rate limit denied", mutate: func(c *Candidate) { c.RateLimitAllowed = false }, wantReason: SkipReasonRateLimitExceeded},
		{name: "concurrency denied", mutate: func(c *Candidate) { c.ConcurrencyAllowed = false }, wantReason: SkipReasonConcurrencyLimitExceeded},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := baseCandidate("route-unavailable")
			tt.mutate(&candidate)
			result, err := Select(baseInput(now, domain.CapabilitySet{}, []Candidate{candidate}))
			if !errors.Is(err, ErrNoRouteAvailable) {
				t.Fatalf("error = %v, want ErrNoRouteAvailable", err)
			}
			assertSkippedReasons(t, result.Skipped, []SkipReason{tt.wantReason})
		})
	}

	accepted := []struct {
		name   string
		mutate func(*Candidate)
	}{
		{name: "cooldown ending now", mutate: func(c *Candidate) { c.Route.CooldownUntil = &now }},
		{name: "cooldown in past", mutate: func(c *Candidate) { c.Route.CooldownUntil = &past }},
		{name: "zero estimated cost", mutate: func(c *Candidate) { c.EstimatedUpstreamCostCents = 0 }},
		{name: "exact balance equal to cost", mutate: func(c *Candidate) { c.EstimatedUpstreamCostCents = 100 }},
	}
	for _, tt := range accepted {
		t.Run(tt.name, func(t *testing.T) {
			candidate := baseCandidate("route-available")
			tt.mutate(&candidate)
			result, err := Select(baseInput(now, domain.CapabilitySet{}, []Candidate{candidate}))
			if err != nil {
				t.Fatalf("Select returned error: %v", err)
			}
			if result.Selected.Route.ID != "route-available" {
				t.Fatalf("selected = %q, want route-available", result.Selected.Route.ID)
			}
		})
	}
}

func TestModelRewritePolicy(t *testing.T) {
	now := fixedNow()
	tests := []struct {
		name       string
		mutate     func(*Candidate)
		wantError  error
		wantReason SkipReason
	}{
		{name: "none equal accepted", mutate: func(c *Candidate) {}},
		{name: "none different skipped", mutate: func(c *Candidate) { c.Route.ProviderModel = "different-model" }, wantError: ErrNoRouteAvailable, wantReason: SkipReasonUnsupportedModelRewritePolicy},
		{name: "provider model allowed accepted", mutate: func(c *Candidate) {
			c.Route.ModelRewritePolicy = domain.ModelRewritePolicyProviderModel
			c.Route.ProviderModel = "provider-model"
			c.ModelIdentifierRewriteAllowed = true
		}},
		{name: "provider model empty skipped", mutate: func(c *Candidate) {
			c.Route.ModelRewritePolicy = domain.ModelRewritePolicyProviderModel
			c.Route.ProviderModel = " "
		}, wantError: ErrNoRouteAvailable, wantReason: SkipReasonUnsupportedModelRewritePolicy},
		{name: "provider model adapter unsupported skipped", mutate: func(c *Candidate) {
			c.Route.ModelRewritePolicy = domain.ModelRewritePolicyProviderModel
			c.Route.ProviderModel = "provider-model"
			c.ModelIdentifierRewriteAllowed = false
		}, wantError: ErrNoRouteAvailable, wantReason: SkipReasonUnsupportedModelRewritePolicy},
		{name: "unknown policy skipped", mutate: func(c *Candidate) { c.Route.ModelRewritePolicy = domain.ModelRewritePolicy("unknown_policy") }, wantError: ErrNoRouteAvailable, wantReason: SkipReasonUnsupportedModelRewritePolicy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidate := baseCandidate("route-rewrite")
			originalClientModel := candidate.Route.ClientModel
			originalProviderModel := candidate.Route.ProviderModel
			tt.mutate(&candidate)
			mutatedClientModel := candidate.Route.ClientModel
			mutatedProviderModel := candidate.Route.ProviderModel
			result, err := Select(baseInput(now, domain.CapabilitySet{}, []Candidate{candidate}))
			if tt.wantError != nil {
				if !errors.Is(err, tt.wantError) {
					t.Fatalf("error = %v, want %v", err, tt.wantError)
				}
				assertSkippedReasons(t, result.Skipped, []SkipReason{tt.wantReason})
			} else if err != nil {
				t.Fatalf("Select returned error: %v", err)
			}
			if candidate.Route.ClientModel != mutatedClientModel || candidate.Route.ProviderModel != mutatedProviderModel {
				t.Fatalf("candidate model fields mutated from %q/%q to %q/%q", mutatedClientModel, mutatedProviderModel, candidate.Route.ClientModel, candidate.Route.ProviderModel)
			}
			if tt.name == "none equal accepted" && (candidate.Route.ClientModel != originalClientModel || candidate.Route.ProviderModel != originalProviderModel) {
				t.Fatalf("baseline model fields mutated")
			}
		})
	}

	wrongFamily := baseCandidate("route-wrong-family")
	wrongFamily.Route.APIFamily = domain.APIFamily("other_family")
	wrongFamily.Route.ModelRewritePolicy = domain.ModelRewritePolicyProviderModel
	wrongFamily.Route.ProviderModel = "provider-model"
	wrongFamily.ModelIdentifierRewriteAllowed = true
	exact := baseCandidate("route-exact")
	result, err := Select(baseInput(now, domain.CapabilitySet{}, []Candidate{wrongFamily, exact}))
	if err != nil {
		t.Fatalf("Select returned error: %v", err)
	}
	if result.Selected.Route.ID != "route-exact" {
		t.Fatalf("selected = %q, want route-exact", result.Selected.Route.ID)
	}
}

func TestDeterministicOrderingAndInputImmutability(t *testing.T) {
	now := fixedNow()
	routeA := baseCandidate("route-a")
	routeA.EstimatedUpstreamCostCents = 10
	routeA.Route.Priority = 2
	routeB := baseCandidate("route-b")
	routeB.EstimatedUpstreamCostCents = 5
	routeB.Route.Priority = 9
	routeC := baseCandidate("route-c")
	routeC.EstimatedUpstreamCostCents = 10
	routeC.Route.Priority = 1
	routeD := baseCandidate("route-d")
	routeD.EstimatedUpstreamCostCents = 10
	routeD.Route.Priority = 1

	candidates := []Candidate{routeA, routeD, routeC, routeB}
	before := cloneCandidates(candidates)
	result, err := Select(baseInput(now, domain.CapabilitySet{}, candidates))
	if err != nil {
		t.Fatalf("Select returned error: %v", err)
	}
	if result.Selected.Route.ID != "route-b" {
		t.Fatalf("selected = %q, want lower cost route-b", result.Selected.Route.ID)
	}
	wantFallbacks := []string{"route-c", "route-d", "route-a"}
	if got := routeIDs(result.Fallbacks); !slices.Equal(got, wantFallbacks) {
		t.Fatalf("fallbacks = %v, want %v", got, wantFallbacks)
	}
	if !reflect.DeepEqual(candidates, before) {
		t.Fatalf("input candidates mutated")
	}

	permuted := []Candidate{routeD, routeC, routeA, routeB}
	result, err = Select(baseInput(now, domain.CapabilitySet{}, permuted))
	if err != nil {
		t.Fatalf("Select returned error for permuted input: %v", err)
	}
	if result.Selected.Route.ID != "route-b" || !slices.Equal(routeIDs(result.Fallbacks), wantFallbacks) {
		t.Fatalf("permuted result selected=%q fallbacks=%v", result.Selected.Route.ID, routeIDs(result.Fallbacks))
	}
}

func TestDuplicateRouteIDRejected(t *testing.T) {
	candidateA := baseCandidate("route-duplicate")
	candidateB := baseCandidate("route-duplicate")
	candidateB.Reseller.ID = "reseller-b"
	candidateB.Route.ResellerID = "reseller-b"

	_, err := Select(baseInput(fixedNow(), domain.CapabilitySet{}, []Candidate{candidateA, candidateB}))
	if !errors.Is(err, ErrInvalidRouteData) {
		t.Fatalf("error = %v, want ErrInvalidRouteData", err)
	}
}

func TestErrorClassificationAndSafety(t *testing.T) {
	now := fixedNow()

	_, err := Select(baseInput(now, domain.CapabilitySet{}, nil))
	if !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("error = %v, want ErrUnknownModel", err)
	}

	missingCap := baseCandidate("route-missing-cap")
	missingCap.Route.Capabilities = domain.CapabilitySet{}
	_, err = Select(baseInput(now, domain.CapabilitySet{Reasoning: true}, []Candidate{missingCap}))
	if !errors.Is(err, ErrUnsupportedCapability) {
		t.Fatalf("error = %v, want ErrUnsupportedCapability", err)
	}

	unavailable := baseCandidate("route-unavailable")
	unavailable.SecretAvailable = false
	_, err = Select(baseInput(now, domain.CapabilitySet{}, []Candidate{unavailable}))
	if !errors.Is(err, ErrNoRouteAvailable) {
		t.Fatalf("error = %v, want ErrNoRouteAvailable", err)
	}

	badRelation := baseCandidate("route-bad-relation")
	badRelation.Route.ResellerID = "different-reseller"
	_, err = Select(baseInput(now, domain.CapabilitySet{}, []Candidate{badRelation}))
	if !errors.Is(err, ErrInvalidRouteData) {
		t.Fatalf("error = %v, want ErrInvalidRouteData", err)
	}

	badProviderType := baseCandidate("route-bad-provider-type")
	badProviderType.Route.ProviderType = domain.ProviderType("different-provider")
	_, err = Select(baseInput(now, domain.CapabilitySet{}, []Candidate{badProviderType}))
	if !errors.Is(err, ErrInvalidRouteData) {
		t.Fatalf("error = %v, want ErrInvalidRouteData", err)
	}

	for _, sentinel := range []error{ErrInvalidSelectionInput, ErrUnknownModel, ErrUnsupportedCapability, ErrNoRouteAvailable, ErrInvalidRouteData} {
		if !errors.Is(fmt.Errorf("wrapped: %w", sentinel), sentinel) {
			t.Fatalf("errors.Is failed for %v", sentinel)
		}
	}

	secretLike := "sk-live-secret-value"
	secretCandidate := baseCandidate("route-secret-safe")
	secretCandidate.Reseller.APIKeyEnv = secretLike
	secretCandidate.SecretAvailable = false
	result, err := Select(baseInput(now, domain.CapabilitySet{}, []Candidate{secretCandidate}))
	if !errors.Is(err, ErrNoRouteAvailable) {
		t.Fatalf("error = %v, want ErrNoRouteAvailable", err)
	}
	if strings.Contains(fmt.Sprint(err), secretLike) || strings.Contains(fmt.Sprintf("%#v", result.Skipped), secretLike) || strings.Contains(fmt.Sprintf("%#v", result), secretLike) {
		t.Fatalf("secret-like value leaked through error or diagnostics")
	}
	if strings.Contains(fmt.Sprint(err), "100") {
		t.Fatalf("balance-like value leaked through error")
	}
}

func TestSecretSafetyTypeShapes(t *testing.T) {
	assertNoFields(t, reflect.TypeOf(Candidate{}), []string{"ResellerAPIKey", "RawAPIKey", "Authorization", "BillingJWT", "ServiceToken", "SecretValue"})
	assertNoFields(t, reflect.TypeOf(SelectionResult{}), []string{"ResellerAPIKey", "RawAPIKey", "Authorization", "BillingJWT", "ServiceToken", "SecretValue"})
	assertNoFields(t, reflect.TypeOf(SkippedRoute{}), []string{"APIKeyEnv", "BalanceCents", "ReservedCents", "MinimumBalanceCents", "Authorization"})
}

func baseInput(now time.Time, requested domain.CapabilitySet, candidates []Candidate) SelectionInput {
	return SelectionInput{
		Query: ports.RouteQuery{
			APIFamily:    domain.APIFamily("family_a"),
			EndpointKind: domain.EndpointKind("kind_a"),
			ClientModel:  "client-model",
		},
		RequestedCapabilities: requested,
		Candidates:            candidates,
		Now:                   now,
	}
}

func baseCandidate(routeID string) Candidate {
	return Candidate{
		Route: domain.Route{
			ID:                 routeID,
			ResellerID:         "reseller-a",
			ProviderType:       domain.ProviderType("provider-a"),
			APIFamily:          domain.APIFamily("family_a"),
			EndpointKind:       domain.EndpointKind("kind_a"),
			ClientModel:        "client-model",
			ProviderModel:      "client-model",
			ModelRewritePolicy: domain.ModelRewritePolicyNone,
			Enabled:            true,
			Priority:           10,
			Capabilities:       domain.CapabilitySet{Chat: true},
		},
		Reseller: domain.Reseller{
			ID:                  "reseller-a",
			ProviderType:        domain.ProviderType("provider-a"),
			APIKeyEnv:           "ROUTE_SECRET_ENV",
			Enabled:             true,
			BalanceCents:        100,
			ReservedCents:       0,
			MinimumBalanceCents: 0,
		},
		SecretAvailable:               true,
		CostAvailable:                 true,
		EstimatedUpstreamCostCents:    50,
		RateLimitAllowed:              true,
		ConcurrencyAllowed:            true,
		ModelIdentifierRewriteAllowed: false,
	}
}

func withClientModel(candidate Candidate, model string) Candidate {
	candidate.Route.ClientModel = model
	candidate.Route.ProviderModel = model
	return candidate
}

func fixedNow() time.Time {
	return time.Date(2026, 6, 12, 10, 0, 0, 0, time.UTC)
}

func routeIDs(candidates []Candidate) []string {
	ids := make([]string, len(candidates))
	for i, candidate := range candidates {
		ids[i] = candidate.Route.ID
	}
	return ids
}

func cloneCandidates(candidates []Candidate) []Candidate {
	clone := make([]Candidate, len(candidates))
	copy(clone, candidates)
	return clone
}

func assertSkippedReasons(t *testing.T, skipped []SkippedRoute, want []SkipReason) {
	t.Helper()
	if len(skipped) != len(want) {
		t.Fatalf("skipped len = %d, want %d: %#v", len(skipped), len(want), skipped)
	}
	for i := range want {
		if skipped[i].Reason != want[i] {
			t.Fatalf("skipped[%d].Reason = %q, want %q", i, skipped[i].Reason, want[i])
		}
	}
}

func assertNoFields(t *testing.T, typ reflect.Type, forbidden []string) {
	t.Helper()
	for _, field := range forbidden {
		if _, ok := typ.FieldByName(field); ok {
			t.Fatalf("%s must not contain field %s", typ.Name(), field)
		}
	}
}
