package pricing

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type fakeEstimator struct {
	estimate ports.TokenEstimate
	err      error
	request  ports.TokenEstimateRequest
	mutate   bool
	calls    int
}

func (f *fakeEstimator) Estimate(_ context.Context, request ports.TokenEstimateRequest) (ports.TokenEstimate, error) {
	f.calls++
	f.request = request
	if f.mutate && len(request.RequestBody) > 0 {
		request.RequestBody[0] = 'X'
	}
	if f.err != nil {
		return ports.TokenEstimate{}, f.err
	}
	return f.estimate, nil
}

func testRoute() domain.Route {
	return domain.Route{ID: "route-1", APIFamily: domain.APIFamily("family"), EndpointKind: domain.EndpointKind("kind"), ClientModel: "client-model", DefaultMaxOutputTokens: 77}
}

func TestPreflightPricer(t *testing.T) {
	calc := newTestCalculator(t)
	if _, err := NewPreflightPricer(nil, calc); !errors.Is(err, ErrInvalidPricingInput) {
		t.Fatalf("nil estimator err %v", err)
	}
	if _, err := NewPreflightPricer(&fakeEstimator{}, nil); !errors.Is(err, ErrInvalidPricingInput) {
		t.Fatalf("nil calculator err %v", err)
	}
	body := []byte(`{"prompt":"secret-token"}`)
	estimator := &fakeEstimator{estimate: ports.TokenEstimate{Usage: domain.TokenUsage{InputTokens: 10_000}, Confidence: "conservative"}, mutate: true}
	pricer, err := NewPreflightPricer(estimator, calc)
	if err != nil {
		t.Fatal(err)
	}
	caps := domain.CapabilitySet{Chat: true, ImageInput: true}
	result, err := pricer.Price(context.Background(), PreflightInput{Route: testRoute(), Price: testPrice(), RequestBody: body, RequestedCapabilities: caps, InputMode: InputPricingModeDetailed})
	if err != nil {
		t.Fatalf("Price error: %v", err)
	}
	if estimator.request.APIFamily != testRoute().APIFamily || estimator.request.EndpointKind != testRoute().EndpointKind || estimator.request.ClientModel != testRoute().ClientModel || estimator.request.DefaultMaxOutputTokens != 77 || !estimator.request.RequestedCapabilities.ImageInput {
		t.Fatalf("estimator request mismatch: %+v", estimator.request)
	}
	if !bytes.Equal(estimator.request.RequestBody, []byte(`X"prompt":"secret-token"}`)) {
		t.Fatalf("fake should mutate only its copy")
	}
	if !bytes.Equal(body, []byte(`{"prompt":"secret-token"}`)) {
		t.Fatalf("caller body mutated: %s", body)
	}
	if result.EstimatedUsage.InputTokens != 12_500 || result.EstimatedUpstreamCostCents == 0 || result.EstimatedClientAmountCents == 0 || result.Currency != "RUB" || result.Confidence != "conservative" {
		t.Fatalf("bad result %+v", result)
	}
	if containsSecret(result, "secret-token") {
		t.Fatalf("secret leaked in preflight result")
	}
}

func TestPreflightValidationErrors(t *testing.T) {
	calc := newTestCalculator(t)
	base := PreflightInput{Route: testRoute(), Price: testPrice(), RequestBody: []byte("{}")}
	pricer, _ := NewPreflightPricer(&fakeEstimator{estimate: ports.TokenEstimate{Usage: domain.TokenUsage{InputTokens: 1}, Confidence: "ok"}}, calc)
	bad := base
	bad.Price.RouteID = "other"
	if _, err := pricer.Price(context.Background(), bad); !errors.Is(err, ErrInvalidPricingInput) {
		t.Fatalf("mismatch err %v", err)
	}
	bad = base
	bad.RequestBody = nil
	if _, err := pricer.Price(context.Background(), bad); !errors.Is(err, ErrInvalidPricingInput) {
		t.Fatalf("nil body err %v", err)
	}
	pricer, _ = NewPreflightPricer(&fakeEstimator{estimate: ports.TokenEstimate{Usage: domain.TokenUsage{InputTokens: -1}, Confidence: "ok"}}, calc)
	if _, err := pricer.Price(context.Background(), base); !errors.Is(err, ErrInvalidUsage) {
		t.Fatalf("negative err %v", err)
	}
	pricer, _ = NewPreflightPricer(&fakeEstimator{estimate: ports.TokenEstimate{Usage: domain.TokenUsage{InputTokens: 1}, Confidence: " \t"}}, calc)
	if _, err := pricer.Price(context.Background(), base); !errors.Is(err, ErrPricingUnavailable) {
		t.Fatalf("blank confidence err %v", err)
	}
	pricer, _ = NewPreflightPricer(&fakeEstimator{err: errors.New("upstream estimator failed")}, calc)
	err := error(nil)
	_, err = pricer.Price(context.Background(), base)
	if !errors.Is(err, ErrPricingUnavailable) {
		t.Fatalf("estimator err %v", err)
	}
	if bytes.Contains([]byte(err.Error()), []byte("{}")) {
		t.Fatalf("raw body leaked: %v", err)
	}
}
