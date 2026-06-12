package pricing

import (
	"errors"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestBillingModel(t *testing.T) {
	got, err := BillingModel(domain.ProviderType("Provider-A"), "Client-Model")
	if err != nil {
		t.Fatalf("BillingModel returned error: %v", err)
	}
	if got != "Provider-A:Client-Model" {
		t.Fatalf("got %q", got)
	}
	if _, err := BillingModel("", "Client-Model"); !errors.Is(err, ErrInvalidPricingInput) {
		t.Fatalf("empty provider err = %v", err)
	}
	if _, err := BillingModel(domain.ProviderType("Provider-A"), " \t"); !errors.Is(err, ErrInvalidPricingInput) {
		t.Fatalf("blank model err = %v", err)
	}
	// The helper accepts only normalized provider_type and client_model inputs; there is no switch on provider_model, reseller ID, or route ID.
}
