package pricing

import (
	"fmt"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func BillingModel(providerType domain.ProviderType, clientModel string) (string, error) {
	if providerType == "" {
		return "", fmt.Errorf("%w: provider type is empty", ErrInvalidPricingInput)
	}
	if strings.TrimSpace(clientModel) == "" {
		return "", fmt.Errorf("%w: client model is blank", ErrInvalidPricingInput)
	}
	return string(providerType) + ":" + clientModel, nil
}
