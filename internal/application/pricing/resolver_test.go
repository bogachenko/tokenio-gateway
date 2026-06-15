package pricing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type fakeExtractor struct {
	result  ports.UsageExtractionResult
	err     error
	request ports.UsageExtractionRequest
	mutate  bool
	calls   int
}

func (f *fakeExtractor) Extract(_ context.Context, request ports.UsageExtractionRequest) (ports.UsageExtractionResult, error) {
	f.calls++
	f.request = request
	if f.mutate {
		if len(request.RequestBody) > 0 {
			request.RequestBody[0] = 'X'
		}
		if len(request.ResponseBody) > 0 {
			request.ResponseBody[0] = 'Y'
		}
	}
	if f.err != nil {
		return ports.UsageExtractionResult{}, f.err
	}
	return f.result, nil
}

func newResolver(t *testing.T, extractor *fakeExtractor, estimator *fakeEstimator) *UsageResolver {
	t.Helper()
	r, err := NewUsageResolver(extractor, estimator, newTestCalculator(t))
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestUsageResolverCompletenessPriority(t *testing.T) {
	price := testPrice()
	route := testRoute()
	input := ResolveUsageInput{Route: route, Price: price, RequestBody: []byte(`{"prompt":"secret-body"}`), ResponseBody: []byte(`{"usage":"secret-body"}`), Modalities: InputModalities{Audio: true}}
	cases := []struct {
		name, completeness string
		usage              domain.TokenUsage
		estimated          bool
		wantCompleteness   UsageCompleteness
		wantUpstream       int64
		estimatorCalls     int
	}{
		{"detailed", "detailed", domain.TokenUsage{InputTokens: 10_000}, false, UsageCompletenessDetailed, 1, 0},
		{"aggregate", "aggregate", domain.TokenUsage{InputTokens: 10_000}, false, UsageCompletenessAggregate, 5, 0},
		{"estimated", "estimated", domain.TokenUsage{InputTokens: 10_000}, true, UsageCompletenessEstimated, 2, 0},
		{"missing", "missing", domain.TokenUsage{}, true, UsageCompletenessEstimated, 2, 1},
		{"failed", "failed", domain.TokenUsage{}, true, UsageCompletenessEstimated, 2, 1},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			presence := ports.UsageDimensionPresence{}
			if tt.completeness == "detailed" ||
				tt.completeness == "aggregate" {
				presence.InputTokens = true
				presence.OutputTokens = true
			}
			extractor := &fakeExtractor{result: ports.UsageExtractionResult{Usage: tt.usage, Presence: presence, Completeness: tt.completeness, ProviderRequestID: "req-1", ProviderResponseModel: "response-model"}}
			estimator := &fakeEstimator{estimate: ports.TokenEstimate{Usage: domain.TokenUsage{InputTokens: 10_000}, Confidence: "ok"}}
			result, err := newResolver(t, extractor, estimator).Resolve(context.Background(), input)
			if err != nil {
				t.Fatalf("Resolve error: %v", err)
			}
			if result.Estimated != tt.estimated || result.Completeness != tt.wantCompleteness || result.UpstreamCostCents != tt.wantUpstream || estimator.calls != tt.estimatorCalls {
				t.Fatalf("result %+v estimator calls %d", result, estimator.calls)
			}
			if tt.estimatorCalls == 0 && (result.ProviderRequestID != "req-1" || result.ProviderResponseModel != "response-model") {
				t.Fatalf("metadata not preserved: %+v", result)
			}
		})
	}
}

func TestUsageResolverMergesOnlyMissingRequiredDimensions(
	t *testing.T,
) {
	route := testRoute()
	route.EndpointKind = domain.EndpointChat
	input := ResolveUsageInput{
		Route:        route,
		Price:        testPrice(),
		RequestBody:  []byte(`{"prompt":"x"}`),
		ResponseBody: []byte(`{"usage":{}}`),
	}
	extractor := &fakeExtractor{
		result: ports.UsageExtractionResult{
			Usage: domain.TokenUsage{
				InputTokens: 1_000_000,
			},
			Presence: ports.UsageDimensionPresence{
				InputTokens: true,
			},
			Completeness:          "aggregate",
			ProviderRequestID:     "req-partial",
			ProviderResponseModel: "response-model",
		},
	}
	estimator := &fakeEstimator{
		estimate: ports.TokenEstimate{
			Usage: domain.TokenUsage{
				InputTokens:     9_000_000,
				OutputTokens:    4_000,
				ReasoningTokens: 4_000,
			},
			Confidence: "conservative",
		},
	}
	result, err := newResolver(
		t,
		extractor,
		estimator,
	).Resolve(context.Background(), input)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if estimator.calls != 1 {
		t.Fatalf("estimator calls = %d", estimator.calls)
	}
	if result.Usage.InputTokens != 1_000_000 {
		t.Fatalf("actual input changed: %+v", result.Usage)
	}
	if result.Usage.OutputTokens != 5_000 {
		t.Fatalf("estimated output = %d", result.Usage.OutputTokens)
	}
	if result.Usage.ReasoningTokens != 0 {
		t.Fatalf("optional reasoning was invented: %+v", result.Usage)
	}
	if !result.Estimated ||
		result.Completeness != UsageCompletenessAggregate {
		t.Fatalf("result = %+v", result)
	}
	if result.UpstreamCostCents != 102 {
		t.Fatalf(
			"upstream cents = %d, want 102",
			result.UpstreamCostCents,
		)
	}
	if result.ProviderRequestID != "req-partial" ||
		result.ProviderResponseModel != "response-model" {
		t.Fatalf("metadata lost: %+v", result)
	}
}

func TestUsageResolverPreservesPresentActualZero(
	t *testing.T,
) {
	route := testRoute()
	route.EndpointKind = domain.EndpointChat
	input := ResolveUsageInput{
		Route:        route,
		Price:        testPrice(),
		RequestBody:  []byte(`{"prompt":"x"}`),
		ResponseBody: []byte(`{"usage":{}}`),
	}
	extractor := &fakeExtractor{
		result: ports.UsageExtractionResult{
			Usage: domain.TokenUsage{
				InputTokens: 0,
			},
			Presence: ports.UsageDimensionPresence{
				InputTokens: true,
			},
			Completeness: "aggregate",
		},
	}
	estimator := &fakeEstimator{
		estimate: ports.TokenEstimate{
			Usage: domain.TokenUsage{
				InputTokens:  10_000,
				OutputTokens: 4_000,
			},
			Confidence: "conservative",
		},
	}
	result, err := newResolver(
		t,
		extractor,
		estimator,
	).Resolve(context.Background(), input)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if result.Usage.InputTokens != 0 ||
		result.Usage.OutputTokens != 5_000 {
		t.Fatalf("usage = %+v", result.Usage)
	}
}

func TestUsageResolverDoesNotEstimateCompleteEmbeddingAggregate(
	t *testing.T,
) {
	route := testRoute()
	route.EndpointKind = domain.EndpointEmbeddings
	input := ResolveUsageInput{
		Route:        route,
		Price:        testPrice(),
		RequestBody:  []byte(`{"input":"x"}`),
		ResponseBody: []byte(`{"usage":{}}`),
	}
	extractor := &fakeExtractor{
		result: ports.UsageExtractionResult{
			Usage: domain.TokenUsage{
				InputTokens: 10_000,
			},
			Presence: ports.UsageDimensionPresence{
				InputTokens: true,
			},
			Completeness: "aggregate",
		},
	}
	estimator := &fakeEstimator{
		estimate: ports.TokenEstimate{
			Usage: domain.TokenUsage{
				InputTokens: 99_999,
			},
			Confidence: "conservative",
		},
	}
	result, err := newResolver(
		t,
		extractor,
		estimator,
	).Resolve(context.Background(), input)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if estimator.calls != 0 ||
		result.Usage.InputTokens != 10_000 ||
		result.Estimated {
		t.Fatalf(
			"result=%+v estimator calls=%d",
			result,
			estimator.calls,
		)
	}
}

func TestUsageResolverFallbacksZeroUsageAndBodySafety(t *testing.T) {
	input := ResolveUsageInput{Route: testRoute(), Price: testPrice(), RequestBody: []byte(`request-secret`), ResponseBody: []byte(`response-secret`)}
	extractor := &fakeExtractor{err: errors.New("extract failed"), mutate: true}
	estimator := &fakeEstimator{estimate: ports.TokenEstimate{Usage: domain.TokenUsage{InputTokens: 10_000}, Confidence: "ok"}, mutate: true}
	result, err := newResolver(t, extractor, estimator).Resolve(context.Background(), input)
	if err != nil {
		t.Fatalf("fallback error: %v", err)
	}
	if !result.Estimated || estimator.calls != 1 {
		t.Fatalf("fallback not used: %+v calls %d", result, estimator.calls)
	}
	if string(input.RequestBody) != `request-secret` || string(input.ResponseBody) != `response-secret` {
		t.Fatalf("caller bodies mutated")
	}
	if !bytes.HasPrefix(extractor.request.RequestBody, []byte("X")) || !bytes.HasPrefix(extractor.request.ResponseBody, []byte("Y")) || !bytes.HasPrefix(estimator.request.RequestBody, []byte("X")) {
		t.Fatalf("fakes did not mutate copies")
	}
	if containsSecret(result, "request-secret") || containsSecret(result, "response-secret") {
		t.Fatalf("secret leaked in resolver result")
	}

	extractor = &fakeExtractor{result: ports.UsageExtractionResult{Usage: domain.TokenUsage{}, Completeness: "detailed"}}
	estimator = &fakeEstimator{estimate: ports.TokenEstimate{Usage: domain.TokenUsage{InputTokens: 10_000}, Confidence: "ok"}}
	result, err = newResolver(t, extractor, estimator).Resolve(context.Background(), input)
	if err != nil || !result.Estimated || estimator.calls != 1 {
		t.Fatalf("zero fallback result %+v err %v", result, err)
	}
	input.ZeroUsageAllowed = true
	estimator = &fakeEstimator{estimate: ports.TokenEstimate{Usage: domain.TokenUsage{InputTokens: 10_000}, Confidence: "ok"}}
	result, err = newResolver(t, extractor, estimator).Resolve(context.Background(), input)
	if err != nil || result.Estimated || result.UpstreamCostCents != 0 || estimator.calls != 0 {
		t.Fatalf("zero allowed result %+v err %v", result, err)
	}
}

func TestUsageResolverErrors(t *testing.T) {
	input := ResolveUsageInput{Route: testRoute(), Price: testPrice(), RequestBody: []byte("raw-secret"), ResponseBody: []byte("raw-secret")}
	estimatorFail := &fakeEstimator{err: errors.New("estimate failed")}
	_, err := newResolver(t, &fakeExtractor{err: errors.New("extract failed")}, estimatorFail).Resolve(context.Background(), input)
	if !errors.Is(err, ErrUsageUnresolved) {
		t.Fatalf("err %v", err)
	}
	if bytes.Contains([]byte(err.Error()), []byte("raw-secret")) {
		t.Fatalf("raw body leaked: %v", err)
	}
	_, err = newResolver(t, &fakeExtractor{result: ports.UsageExtractionResult{Usage: domain.TokenUsage{InputTokens: -1}, Completeness: "detailed"}}, &fakeEstimator{estimate: ports.TokenEstimate{Usage: domain.TokenUsage{InputTokens: 1}, Confidence: "ok"}}).Resolve(context.Background(), input)
	if !errors.Is(err, ErrInvalidUsage) {
		t.Fatalf("negative usage err %v", err)
	}
	_, err = newResolver(t, &fakeExtractor{result: ports.UsageExtractionResult{Usage: domain.TokenUsage{InputTokens: 1}, Completeness: "unknown"}}, &fakeEstimator{}).Resolve(context.Background(), input)
	if !errors.Is(err, ErrInvalidUsageCompleteness) {
		t.Fatalf("unknown completeness err %v", err)
	}
	_, err = NewUsageResolver(nil, &fakeEstimator{}, newTestCalculator(t))
	if !errors.Is(err, ErrInvalidPricingInput) {
		t.Fatalf("nil extractor err %v", err)
	}
	_, err = NewUsageResolver(&fakeExtractor{}, nil, newTestCalculator(t))
	if !errors.Is(err, ErrInvalidPricingInput) {
		t.Fatalf("nil estimator err %v", err)
	}
}

func containsSecret(v any, secret string) bool {
	return bytes.Contains([]byte(fmt.Sprintf("%+v", v)), []byte(secret))
}

func TestResolvedDTOShape(t *testing.T) { _ = reflect.TypeOf(ResolvedUsageResult{}) }
