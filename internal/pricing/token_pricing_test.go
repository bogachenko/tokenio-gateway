package pricing

import (
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestMultiplyUsageDoesNotApplyTokenSafetyFactorToImageGenerationUnits(t *testing.T) {
	usage := multiplyUsage(domain.TokenUsage{ImageGenerationUnits: 4}, 2.5)

	if usage.ImageGenerationUnits != 4 {
		t.Fatalf("ImageGenerationUnits safety factor mismatch: got %d, want 4", usage.ImageGenerationUnits)
	}
}
