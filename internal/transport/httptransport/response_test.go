package httptransport

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestWriteUpstreamSuccess(t *testing.T) {
	tests := []struct {
		status int
		body   string
	}{
		{status: http.StatusOK, body: "{\"ok\":true}"},
		{status: http.StatusCreated, body: "created bytes"},
		{status: 299, body: "edge success"},
	}
	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			headers := http.Header{}
			headers.Set("Content-Type", "application/json")
			headers.Set("X-Safe-Header", "safe")
			recorder := httptest.NewRecorder()
			if err := WriteUpstreamSuccess(recorder, tt.status, headers, []byte(tt.body)); err != nil {
				t.Fatal(err)
			}
			if recorder.Code != tt.status {
				t.Fatalf("status = %d, want %d", recorder.Code, tt.status)
			}
			if recorder.Body.String() != tt.body {
				t.Fatalf("body = %q, want %q", recorder.Body.String(), tt.body)
			}
			if got := recorder.Header().Get("Content-Type"); got != "application/json" {
				t.Fatalf("Content-Type = %q", got)
			}
			if got := recorder.Header().Get("X-Safe-Header"); got != "safe" {
				t.Fatalf("X-Safe-Header = %q", got)
			}
		})
	}
}

func TestWriteUpstreamSuccessRejectsNonSuccessWithoutWrite(t *testing.T) {
	for _, status := range []int{199, 300, 400, 500} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			headers := http.Header{"Content-Type": []string{"application/json"}}
			recorder := httptest.NewRecorder()
			err := WriteUpstreamSuccess(recorder, status, headers, []byte("must not write"))
			if !errors.Is(err, ErrNonSuccessfulUpstreamStatus) {
				t.Fatalf("err = %v", err)
			}
			if recorder.Body.Len() != 0 {
				t.Fatalf("body was written: %q", recorder.Body.String())
			}
			if got := recorder.Header().Get("Content-Type"); got != "" {
				t.Fatalf("header was written: %q", got)
			}
		})
	}
}

func TestWriteUpstreamSuccessFiltersHeaders(t *testing.T) {
	headers := http.Header{}
	headers.Set("Content-Type", "application/json")
	headers.Set("Content-Length", "123")
	headers.Set("connection", "close")
	headers.Set("Transfer-Encoding", "chunked")
	headers.Set("Upgrade", "websocket")
	headers.Set("Proxy-Authenticate", "Basic")
	headers.Set("Proxy-Authorization", "Basic secret")
	headers.Set("TE", "trailers")
	headers.Set("Trailer", "Expires")
	headers.Set("Keep-Alive", "timeout=5")
	headers.Set("x-local-request-id", "upstream")
	headers.Set("x-billing-amount-cents", "100")
	headers.Set("X-Wallet-Balance-Cents", "100")
	headers.Set("X-Ordinary", "ordinary")

	recorder := httptest.NewRecorder()
	if err := WriteUpstreamSuccess(recorder, http.StatusOK, headers, []byte("ok")); err != nil {
		t.Fatal(err)
	}

	for _, key := range []string{
		"Content-Length",
		"Connection",
		"Transfer-Encoding",
		"Upgrade",
		"Proxy-Authenticate",
		"Proxy-Authorization",
		"TE",
		"Trailer",
		"Keep-Alive",
		"X-Local-Request-ID",
		"X-Billing-Amount-Cents",
		"X-Wallet-Balance-Cents",
	} {
		if got := recorder.Header().Get(key); got != "" {
			t.Fatalf("%s copied as %q", key, got)
		}
	}
	if got := recorder.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := recorder.Header().Get("X-Ordinary"); got != "ordinary" {
		t.Fatalf("X-Ordinary = %q", got)
	}
}

func TestSetBillingHeaders(t *testing.T) {
	headers := http.Header{}
	SetBillingHeaders(headers, BillingHeaders{
		LocalRequestID:              "llmreq_1",
		ProviderType:                domain.ProviderOpenAI,
		ClientModel:                 "gpt-client",
		BillingModel:                "gpt-billing",
		InputTokens:                 1,
		CachedInputTokens:           2,
		OutputTokens:                3,
		ReasoningTokens:             4,
		ImageInputTokens:            5,
		AudioInputTokens:            6,
		AudioOutputTokens:           7,
		FileInputTokens:             8,
		VideoInputTokens:            9,
		ImageGenerationUnits:        10,
		ClientAmountCents:           11,
		Currency:                    "RUB",
		WalletBalanceCents:          12,
		WalletEffectiveBalanceCents: 13,
		BillingPendingCents:         14,
	})

	want := map[string]string{
		"X-Local-Request-ID":               "llmreq_1",
		"X-Billing-Provider-Type":          "openai",
		"X-Billing-Client-Model":           "gpt-client",
		"X-Billing-Model":                  "gpt-billing",
		"X-Billing-Input-Tokens":           "1",
		"X-Billing-Cached-Input-Tokens":    "2",
		"X-Billing-Output-Tokens":          "3",
		"X-Billing-Reasoning-Tokens":       "4",
		"X-Billing-Image-Input-Tokens":     "5",
		"X-Billing-Audio-Input-Tokens":     "6",
		"X-Billing-Audio-Output-Tokens":    "7",
		"X-Billing-File-Input-Tokens":      "8",
		"X-Billing-Video-Input-Tokens":     "9",
		"X-Billing-Image-Generation-Units": "10",
		"X-Billing-Amount-Cents":           "11",
		"X-Billing-Currency":               "RUB",
		"X-Wallet-Balance-Cents":           "12",
		"X-Wallet-Effective-Balance-Cents": "13",
		"X-Billing-Pending-Cents":          "14",
	}
	for key, value := range want {
		if got := headers.Get(key); got != value {
			t.Fatalf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestSetBillingHeadersWritesZeroNumericsAndOmitsEmptyStrings(t *testing.T) {
	headers := http.Header{}
	SetBillingHeaders(headers, BillingHeaders{})

	if got := headers.Get("X-Billing-Input-Tokens"); got != "0" {
		t.Fatalf("zero numeric header = %q", got)
	}
	if got := headers.Get("X-Billing-Image-Generation-Units"); got != "0" {
		t.Fatalf("image generation units = %q", got)
	}
	for _, key := range []string{"X-Local-Request-ID", "X-Billing-Provider-Type", "X-Billing-Client-Model", "X-Billing-Model", "X-Billing-Currency"} {
		if got := headers.Get(key); got != "" {
			t.Fatalf("empty optional %s = %q", key, got)
		}
	}
}
