package pricing

import "errors"

var ErrInvalidPricingInput = errors.New("invalid pricing input")
var ErrInvalidUsage = errors.New("invalid usage")
var ErrUnsupportedCurrency = errors.New("unsupported currency")
var ErrInvalidMarkup = errors.New("invalid markup")
var ErrMissingPrice = errors.New("missing price")
var ErrInvalidImageGenerationPricing = errors.New("invalid image generation pricing")
var ErrPricingUnavailable = errors.New("pricing unavailable")
var ErrUsageUnresolved = errors.New("usage unresolved")
var ErrInvalidUsageCompleteness = errors.New("invalid usage completeness")
var ErrAmountOverflow = errors.New("amount overflow")
