package openaicompat

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type stage10V5AdapterRoundTripFunc func(*http.Request) (*http.Response, error)

func (f stage10V5AdapterRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestStage10V5ProviderFailureDoesNotExposeRawResponseBody(t *testing.T) {
	const secretBody = `{"error":"provider-secret"}`
	reseller := domain.Reseller{
		ID:           "reseller_1",
		ProviderType: domain.ProviderOpenAI,
		BaseURL:      "https://provider.example",
	}
	adapter, err := NewAdapter(Config{
		Reseller:       reseller,
		ResellerAPIKey: "provider-key",
		Transport: stage10V5AdapterRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusInternalServerError,
				Header:     make(http.Header),
				Body:       io.NopCloser(strings.NewReader(secretBody)),
			}, nil
		}),
		MaxResponseBodyBytes: 1024,
	}, StatusClassifier{})
	if err != nil {
		t.Fatal(err)
	}
	route := domain.Route{
		ID:                 "route_1",
		ResellerID:         reseller.ID,
		ProviderType:       reseller.ProviderType,
		APIFamily:          domain.APIFamilyOpenAICompatible,
		EndpointKind:       domain.EndpointChat,
		ClientModel:        "model-a",
		ProviderModel:      "model-a",
		ModelRewritePolicy: domain.ModelRewritePolicyNone,
	}
	_, err = adapter.Forward(context.Background(), ports.ForwardRequest{
		Route:  route,
		Method: http.MethodPost,
		Path:   "/v1/chat/completions",
		Body:   []byte(`{"model":"model-a"}`),
	})
	if err == nil {
		t.Fatal("expected provider failure")
	}
	if strings.Contains(err.Error(), secretBody) || strings.Contains(err.Error(), "provider-secret") {
		t.Fatalf("raw provider body leaked: %v", err)
	}
}

func TestStage10V5AdapterBaseURLUsesCanonicalDomainContract(t *testing.T) {
	for _, invalid := range []string{
		"provider.example",
		"ftp://provider.example",
		"https://user:password@provider.example",
		"https://provider.example?token=secret",
		"https://provider.example#fragment",
	} {
		if _, err := parseBaseURL(invalid); err == nil {
			t.Fatalf("invalid adapter base URL accepted: %q", invalid)
		}
	}
}
