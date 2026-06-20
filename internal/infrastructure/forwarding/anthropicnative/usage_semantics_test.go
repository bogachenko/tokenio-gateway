package anthropicnative

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/application/forwarding"
)

func TestAnthropicNativeSuccessWithUsagePopulatesForwardUsage(t *testing.T) {
	responseBody := `{"id":"msg_1","usage":{"input_tokens":12,"output_tokens":7}}`
	adapter := newTestAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
		}, nil
	}))

	response, err := adapter.Forward(t.Context(), baseForwardRequest())
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if string(response.Body) != responseBody {
		t.Fatalf("body changed: %s", response.Body)
	}
	if response.Usage == nil {
		t.Fatal("usage is nil")
	}
	if response.Usage.InputTokens != 12 || response.Usage.OutputTokens != 7 {
		t.Fatalf("usage = %#v", response.Usage)
	}
}

func TestAnthropicNativeSuccessWithoutUsageLeavesForwardUsageNil(t *testing.T) {
	responseBody := `{"id":"msg_1","content":[{"type":"text","text":"hello"}]}`
	adapter := newTestAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
		}, nil
	}))

	response, err := adapter.Forward(t.Context(), baseForwardRequest())
	if err != nil {
		t.Fatalf("Forward: %v", err)
	}
	if response.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", response.StatusCode)
	}
	if response.Headers["Content-Type"][0] != "application/json" {
		t.Fatalf("headers = %#v", response.Headers)
	}
	if string(response.Body) != responseBody {
		t.Fatalf("body changed: %s", response.Body)
	}
	if response.Usage != nil {
		t.Fatalf("usage = %#v, want nil", response.Usage)
	}
}

func TestAnthropicNativeMalformedUsageStillFailsAsMalformedResponse(t *testing.T) {
	responseBody := `{"id":"msg_1","usage":{"input_tokens":"12","output_tokens":7}}`
	adapter := newTestAdapter(t, roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     http.Header{"Content-Type": {"application/json"}},
			Body:       io.NopCloser(strings.NewReader(responseBody)),
		}, nil
	}))

	_, err := adapter.Forward(t.Context(), baseForwardRequest())
	var failure *forwarding.Failure
	if !errors.As(err, &failure) {
		t.Fatalf("failure = %#v err=%v", failure, err)
	}
	if failure.Kind != forwarding.FailureKindMalformedResponse || failure.AttemptState != forwarding.AttemptStateResponseReceived || failure.StatusCode != http.StatusOK {
		t.Fatalf("failure = %#v", failure)
	}
}
