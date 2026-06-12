package routing

type SkipReason string

const (
	SkipReasonManualDisabled                SkipReason = "manual_disabled"
	SkipReasonMissingResellerAPIKey         SkipReason = "missing_reseller_api_key"
	SkipReasonCooldownActive                SkipReason = "cooldown_active"
	SkipReasonMissingCapability             SkipReason = "missing_capability"
	SkipReasonInsufficientResellerBalance   SkipReason = "insufficient_reseller_balance"
	SkipReasonRateLimitExceeded             SkipReason = "rate_limit_exceeded"
	SkipReasonConcurrencyLimitExceeded      SkipReason = "concurrency_limit_exceeded"
	SkipReasonUnsupportedModelRewritePolicy SkipReason = "unsupported_model_rewrite_policy"
	SkipReasonInvalidRoutePrice             SkipReason = "invalid_route_price"
	SkipReasonPricingUnavailable            SkipReason = "pricing_unavailable"
)

type SkippedRoute struct {
	RouteID    string
	ResellerID string
	Reason     SkipReason
}
