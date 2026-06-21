package ollamanative

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

func TestOllamaNativeTransportErrorBeforeWriteIsTypedRetryableFailure(t *testing.T) {
	cause := errors.New("dial failed")
	adapter := newOllamaTransportFailureAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	}))

	_, err := adapter.Forward(context.Background(), baseOllamaForwardRequest())
	failure := assertOllamaForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindConnectionError ||
		failure.AttemptState != forwarding.AttemptStateNotSent ||
		!failure.RouteRetryCandidate ||
		!errors.Is(err, cause) {
		t.Fatalf("failure=%+v err=%v", failure, err)
	}
}

func TestOllamaNativeTransportErrorAfterWriteIsUncertainNonRetryableFailure(t *testing.T) {
	cause := errors.New("transport failed after body read")
	adapter := newOllamaTransportFailureAdapter(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		_, _ = io.ReadAll(request.Body)
		return nil, cause
	}))

	_, err := adapter.Forward(context.Background(), baseOllamaForwardRequest())
	failure := assertOllamaForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindUncertainProcessing ||
		failure.AttemptState != forwarding.AttemptStateSentNoResponse ||
		failure.RouteRetryCandidate ||
		!errors.Is(err, cause) {
		t.Fatalf("failure=%+v err=%v", failure, err)
	}
}

func TestOllamaNativeNilResponseNilErrorIsTypedMalformedRetryableFailure(t *testing.T) {
	adapter := newOllamaTransportFailureAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	}))

	_, err := adapter.Forward(context.Background(), baseOllamaForwardRequest())
	failure := assertOllamaForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindMalformedResponse ||
		failure.AttemptState != forwarding.AttemptStateNotSent ||
		!failure.RouteRetryCandidate {
		t.Fatalf("failure=%+v", failure)
	}
}

func TestOllamaNativeResponsePlusErrorKeepsResponseClassificationAndCause(t *testing.T) {
	cause := errors.New("transport returned response and error")
	adapter := newOllamaTransportFailureAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": {"5"}},
			Body:       io.NopCloser(strings.NewReader(`{"error":"rate limited"}`)),
		}, cause
	}))

	_, err := adapter.Forward(context.Background(), baseOllamaForwardRequest())
	failure := assertOllamaForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindRateLimited ||
		failure.StatusCode != http.StatusTooManyRequests ||
		failure.AttemptState != forwarding.AttemptStateResponseReceived ||
		!failure.RouteRetryCandidate ||
		failure.FailureRetryAfterDelay().Seconds() != 5 ||
		!errors.Is(err, cause) {
		t.Fatalf("failure=%+v err=%v", failure, err)
	}
}

func newOllamaTransportFailureAdapter(t *testing.T, transport http.RoundTripper) *Adapter {
	t.Helper()
	adapter, err := NewAdapter(Config{
		Reseller:             domain.Reseller{ID: "reseller-ollama", ProviderType: domain.ProviderOllama, BaseURL: "https://ollama.example"},
		ResellerAPIKey:       "provider-secret",
		Transport:            transport,
		MaxResponseBodyBytes: 1024,
	})
	if err != nil {
		t.Fatalf("NewAdapter: %v", err)
	}
	return adapter
}

func baseOllamaForwardRequest() ports.ForwardRequest {
	return ports.ForwardRequest{
		Route:  ollamaRoute(domain.EndpointChat, "llama-client", "llama-client", domain.ModelRewritePolicyNone),
		Method: http.MethodPost,
		Path:   "/api/chat",
		Body:   []byte(`{"model":"llama-client","messages":[]}`),
	}
}

func assertOllamaForwardingFailure(t *testing.T, err error) *forwarding.Failure {
	t.Helper()
	var failure *forwarding.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("err=%v, want forwarding failure", err)
	}
	return failure
}
