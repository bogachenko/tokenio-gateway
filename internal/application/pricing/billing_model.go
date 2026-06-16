package pricing

import (
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func BillingModel(
	providerType domain.ProviderType,
	clientModel string,
) (string, error) {
	value, err := domain.BillingModel(providerType, clientModel)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrInvalidPricingInput, err)
	}
	return value, nil
}
