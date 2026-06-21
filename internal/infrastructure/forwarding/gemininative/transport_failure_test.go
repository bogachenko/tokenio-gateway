package gemininative

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/application/forwarding"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestGeminiNativeTransportErrorBeforeWriteIsTypedRetryableFailure(t *testing.T) {
	cause := errors.New("dial failed")
	adapter := newGeminiTransportFailureAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	}))

	_, err := adapter.Forward(context.Background(), baseGeminiForwardRequest())
	failure := assertGeminiForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindConnectionError ||
		failure.AttemptState != forwarding.AttemptStateNotSent ||
		!failure.RouteRetryCandidate ||
		!errors.Is(err, cause) {
		t.Fatalf("failure=%+v err=%v", failure, err)
	}
}

func TestGeminiNativeTransportErrorAfterWriteIsUncertainNonRetryableFailure(t *testing.T) {
	cause := errors.New("transport failed after body read")
	adapter := newGeminiTransportFailureAdapter(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		_, _ = io.ReadAll(request.Body)
		return nil, cause
	}))

	_, err := adapter.Forward(context.Background(), baseGeminiForwardRequest())
	failure := assertGeminiForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindUncertainProcessing ||
		failure.AttemptState != forwarding.AttemptStateSentNoResponse ||
		failure.RouteRetryCandidate ||
		!errors.Is(err, cause) {
		t.Fatalf("failure=%+v err=%v", failure, err)
	}
}

func TestGeminiNativeNilResponseNilErrorIsTypedMalformedRetryableFailure(t *testing.T) {
	adapter := newGeminiTransportFailureAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	}))

	_, err := adapter.Forward(context.Background(), baseGeminiForwardRequest())
	failure := assertGeminiForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindMalformedResponse ||
		failure.AttemptState != forwarding.AttemptStateNotSent ||
		!failure.RouteRetryCandidate {
		t.Fatalf("failure=%+v", failure)
	}
}

func TestGeminiNativeResponsePlusErrorKeepsResponseClassificationAndCause(t *testing.T) {
	cause := errors.New("transport returned response and error")
	adapter := newGeminiTransportFailureAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": {"4"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":{"status":"RESOURCE_EXHAUSTED"}}`)),
		}, cause
	}))

	_, err := adapter.Forward(context.Background(), baseGeminiForwardRequest())
	failure := assertGeminiForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindRateLimited ||
		failure.StatusCode != http.StatusTooManyRequests ||
		failure.AttemptState != forwarding.AttemptStateResponseReceived ||
		!failure.RouteRetryCandidate ||
		failure.FailureRetryAfterDelay().Seconds() != 4 ||
		!errors.Is(err, cause) {
		t.Fatalf("failure=%+v err=%v", failure, err)
	}
}

func newGeminiTransportFailureAdapter(t *testing.T, transport http.RoundTripper) *Adapter {
	t.Helper()
	adapter, err := NewAdapter(Config{
		Reseller:             domain.Reseller{ID: "reseller-gemini", ProviderType: domain.ProviderGemini, BaseURL: "https://gemini.example"},
		ResellerAPIKey:       "provider-secret",
		Transport:            transport,
		MaxResponseBodyBytes: 1024,
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	return adapter
}

func baseGeminiForwardRequest() ports.ForwardRequest {
	return ports.ForwardRequest{
		Route:  geminiRoute(domain.EndpointChat, "gemini-client", "gemini-client", domain.ModelRewritePolicyNone),
		Method: http.MethodPost,
		Path:   "/v1beta/models/gemini-client:generateContent",
		Body:   []byte(`{"contents":[]}`),
	}
}

func assertGeminiForwardingFailure(t *testing.T, err error) *forwarding.Failure {
	t.Helper()
	var failure *forwarding.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("err=%v, want forwarding failure", err)
	}
	return failure
}
