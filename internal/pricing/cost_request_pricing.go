package pricing

import (
	"fmt"
	"math"
)

type Calculator struct {
	MarkupCoefficient float64
	Currency          string
}

func (c Calculator) AmountCents(costRequest float64, freeRequest bool) (int64, error) {
	if freeRequest || costRequest == 0 {
		return 0, nil
	}
	if costRequest < 0 {
		return 0, fmt.Errorf("cost_request must be non-negative")
	}
	if c.Currency != "RUB" {
		return 0, fmt.Errorf("unsupported currency: %s", c.Currency)
	}
	if c.MarkupCoefficient <= 0 {
		return 0, fmt.Errorf("markup coefficient must be positive")
	}
	return int64(math.Ceil(costRequest * c.MarkupCoefficient * 100)), nil
}
