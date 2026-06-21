package anthropicnative

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/application/forwarding"
)

func TestAnthropicNativeTransportErrorBeforeWriteIsTypedRetryableFailure(t *testing.T) {
	cause := errors.New("dial failed")
	adapter := newTestAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, cause
	}))

	_, err := adapter.Forward(context.Background(), baseForwardRequest())
	failure := assertForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindConnectionError ||
		failure.AttemptState != forwarding.AttemptStateNotSent ||
		!failure.RouteRetryCandidate ||
		!errors.Is(err, cause) {
		t.Fatalf("failure=%+v err=%v", failure, err)
	}
}

func TestAnthropicNativeTransportErrorAfterWriteIsUncertainNonRetryableFailure(t *testing.T) {
	cause := errors.New("connection reset after write")
	adapter := newTestAdapter(t, roundTripFunc(func(request *http.Request) (*http.Response, error) {
		_, _ = io.ReadAll(request.Body)
		return nil, cause
	}))

	_, err := adapter.Forward(context.Background(), baseForwardRequest())
	failure := assertForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindUncertainProcessing ||
		failure.AttemptState != forwarding.AttemptStateSentNoResponse ||
		failure.RouteRetryCandidate ||
		!errors.Is(err, cause) {
		t.Fatalf("failure=%+v err=%v", failure, err)
	}
}

func TestAnthropicNativeNilResponseNilErrorIsTypedMalformedRetryableFailure(t *testing.T) {
	adapter := newTestAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, nil
	}))

	_, err := adapter.Forward(context.Background(), baseForwardRequest())
	failure := assertForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindMalformedResponse ||
		failure.AttemptState != forwarding.AttemptStateNotSent ||
		!failure.RouteRetryCandidate {
		t.Fatalf("failure=%+v", failure)
	}
}

func TestAnthropicNativeResponsePlusErrorKeepsResponseClassificationAndCause(t *testing.T) {
	cause := errors.New("transport returned response and error")
	adapter := newTestAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusTooManyRequests,
			Header:     http.Header{"Retry-After": {"3"}},
			Body:       io.NopCloser(strings.NewReader(`{"type":"error","error":{"type":"rate_limit_error"}}`)),
		}, cause
	}))

	_, err := adapter.Forward(context.Background(), baseForwardRequest())
	failure := assertForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindRateLimited ||
		failure.StatusCode != http.StatusTooManyRequests ||
		failure.AttemptState != forwarding.AttemptStateResponseReceived ||
		!failure.RouteRetryCandidate ||
		failure.FailureRetryAfterDelay() != 3*time.Second ||
		!errors.Is(err, cause) {
		t.Fatalf("failure=%+v err=%v", failure, err)
	}
}

func TestAnthropicNativeDeadlineBeforeSendIsTypedTimeoutFailure(t *testing.T) {
	adapter := newTestAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("transport must not be called")
		return nil, nil
	}))
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := adapter.Forward(ctx, baseForwardRequest())
	failure := assertForwardingFailure(t, err)
	if failure.Kind != forwarding.FailureKindTimeout ||
		failure.AttemptState != forwarding.AttemptStateNotSent ||
		!failure.RouteRetryCandidate ||
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("failure=%+v err=%v", failure, err)
	}
}

func assertForwardingFailure(t *testing.T, err error) *forwarding.Failure {
	t.Helper()
	var failure *forwarding.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("err=%v, want forwarding failure", err)
	}
	return failure
}
