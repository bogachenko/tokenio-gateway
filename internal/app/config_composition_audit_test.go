package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestConfigFieldsReachCompositionRootConsumers is a composition-root audit.
//
// The test is intentionally package-level rather than single-file-level:
// app wiring often passes config.Config through a constructor and the actual
// consumer is a child graph builder in the same composition package. A field
// should appear in internal/app code when it is intentionally consumed there.
// Logging config is explicitly excluded here because it belongs to the next
// plan step: 3.2 Structured logging/redaction.
func TestConfigFieldsReachCompositionRootConsumers(t *testing.T) {
	appSource := readAppGoSources(t)

	consumedFields := []string{
		"GatewayAddr",
		"DatabaseDSN",
		"BillingBaseURL",
		"BillingServiceToken",
		"BillingJWTSigningKey",
		"BillingJWTTTL",
		"BillingTimeout",
		"BillingRecoveryInterval",
		"BillingRecoveryBatchSize",
		"AdminToken",
		"APIKeyHashSecret",
		"APIKeyLastUsedTimeout",
		"ProvisioningServiceToken",
		"APIKeyProvisioningEncryptionKey",
		"APIKeyProvisioningKeyVersion",
		"APIKeyProvisioningTTL",
		"APIKeyProvisioningExpirationInterval",
		"APIKeyProvisioningExpirationBatchSize",
		"ForwardingAttemptRecoveryStaleAfter",
		"ForwardingAttemptRecoveryInterval",
		"ForwardingAttemptRecoveryBatchSize",
		"TelegramStaleAttemptRecoveryStaleAfter",
		"TelegramStaleAttemptRecoveryInterval",
		"TelegramStaleAttemptRecoveryBatchSize",
		"TelegramDeliveryInterval",
		"TelegramDeliveryBatchSize",
		"TelegramFailedRetryInterval",
		"TelegramFailedRetryBatchSize",
		"TelegramBalanceScanInterval",
		"TelegramBalanceScanBatchSize",
		"CostCurrency",
		"AutoChargeThresholdCents",
		"MinChargeAmountCents",
		"MinRequestBalanceCents",
		"TokenEstimationSafetyFactor",
		"CostEstimationSafetyFactor",
		"RequestBodyMaxBytes",
		"UpstreamResponseBodyMaxBytes",
		"TelegramBotToken",
		"TelegramChatID",
		"TelegramAlertDedupePeriod",
		"ResellerBalanceAlertCents",
		"CooldownRateLimit",
		"CooldownQuotaExceeded",
		"Cooldown5XX",
		"CooldownTimeout",
		"CooldownAuthError",
		"UpstreamTimeout",
		"UpstreamMaxAttempts",
		"UpstreamMaxBackoff",
		"RateLimitMaxWait",
		"HTTPReadHeaderTimeout",
		"HTTPReadTimeout",
		"HTTPWriteTimeout",
		"HTTPIdleTimeout",
		"HTTPShutdownTimeout",
	}

	for _, field := range consumedFields {
		field := field
		t.Run(field, func(t *testing.T) {
			if !strings.Contains(appSource, "."+field) {
				t.Fatalf("config field %s has no composition-root consumer evidence in internal/app", field)
			}
		})
	}
}

func TestLoggingConfigFieldsRemainStructuredLoggingScope(t *testing.T) {
	appSource := readAppGoSources(t)
	pendingStructuredLoggingFields := []string{
		"LogLevel",
		"LogFormat",
		"LogBodies",
	}

	for _, field := range pendingStructuredLoggingFields {
		field := field
		t.Run(field, func(t *testing.T) {
			if strings.Contains(appSource, "."+field) {
				t.Fatalf("logging config field %s is now consumed in internal/app; move it from the 3.2 pending scope into the consumer evidence list", field)
			}
		})
	}
}

func readAppGoSources(t *testing.T) string {
	t.Helper()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob app sources: %v", err)
	}
	var builder strings.Builder
	for _, match := range matches {
		if strings.HasSuffix(match, "_test.go") {
			continue
		}
		content, err := os.ReadFile(match)
		if err != nil {
			t.Fatalf("read %s: %v", match, err)
		}
		builder.WriteString("\n// ")
		builder.WriteString(match)
		builder.WriteString("\n")
		builder.Write(content)
	}
	return builder.String()
}
