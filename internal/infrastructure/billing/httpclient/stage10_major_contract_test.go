package httpclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

type stage10MajorRoundTripFunc func(*http.Request) (*http.Response, error)

func (f stage10MajorRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestStage10MajorBillingHTTPErrorDoesNotExposeRawResponseBody(t *testing.T) {
	const secretBody = `{"error":"upstream-secret"}`
	client, err := New(Config{
		BaseURL:      "https://billing.example",
		ServiceToken: "service-token",
		RoundTripper: stage10MajorRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Body:       io.NopCloser(strings.NewReader(secretBody)),
				Header:     make(http.Header),
			}, nil
		}),
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
