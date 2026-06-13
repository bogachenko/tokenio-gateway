package httpclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type stage10V5RoundTripFunc func(*http.Request) (*http.Response, error)

func (f stage10V5RoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestStage10V5BillingHTTPErrorDoesNotExposeRawResponseBody(t *testing.T) {
	const secretBody = `{"error":"upstream-secret"}`
	client, err := New(Config{
		BaseURL:      "https://billing.example",
		ServiceToken: "service-token",
		RoundTripper: stage10V5RoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(secretBody)),
				Header:     make(http.Header),
			}, nil
		}),
		Timeout:              time.Second,
		MaxResponseBodyBytes: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.GetBalance(context.Background(), "billing-jwt")
	if err == nil {
		t.Fatal("expected billing error")
	}
	if strings.Contains(err.Error(), secretBody) || strings.Contains(err.Error(), "upstream-secret") {
		t.Fatalf("raw billing body leaked: %v", err)
	}
}
