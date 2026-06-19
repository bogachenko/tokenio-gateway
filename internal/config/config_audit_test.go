package config

import (
	"os"
	"regexp"
	"sort"
	"testing"
)

func TestConfigurationSpecEnvKeysAreConsumed(t *testing.T) {
	specText := readAuditFile(t, "../../docs/spec/090-configuration.ru.md")
	configText := readAuditFile(t, "config.go")

	documented := collectTokenioEnvKeys(specText)
	consumed := collectTokenioEnvKeys(configText)
	nonRuntime := documentedButNotRuntimeConfigKeys()

	for _, key := range sortedKeys(documented) {
		if _, ok := nonRuntime[key]; ok {
			continue
		}
		if _, ok := consumed[key]; !ok {
			t.Fatalf("documented env key %s is not consumed by internal/config", key)
		}
	}
}

func TestConfigurationLoaderEnvKeysAreDocumentedOrExplicitlyPending(t *testing.T) {
	specText := readAuditFile(t, "../../docs/spec/090-configuration.ru.md")
	configText := readAuditFile(t, "config.go")

	documented := collectTokenioEnvKeys(specText)
	consumed := collectTokenioEnvKeys(configText)
	pendingSpec := implementationOnlyEnvKeysPendingSpecReconciliation()

	for _, key := range sortedKeys(consumed) {
		if _, ok := documented[key]; ok {
			continue
		}
		if _, ok := pendingSpec[key]; ok {
			continue
		}
		t.Fatalf("internal/config consumes undocumented env key %s", key)
	}

	for _, key := range sortedKeys(pendingSpec) {
		if _, ok := consumed[key]; !ok {
			t.Fatalf("pending spec env key %s is no longer consumed; remove it from audit allowlist", key)
		}
	}
}

func TestConfigurationSpecNonRuntimeExclusionsStayExplicit(t *testing.T) {
	nonRuntime := documentedButNotRuntimeConfigKeys()
	if len(nonRuntime) != 1 {
		t.Fatalf("non-runtime config exclusions = %v, want only TOKENIO_MIGRATIONS_DIR", sortedKeys(nonRuntime))
	}
	if _, ok := nonRuntime["TOKENIO_MIGRATIONS_DIR"]; !ok {
		t.Fatalf("TOKENIO_MIGRATIONS_DIR must remain the only documented non-runtime config exclusion")
	}
}

func documentedButNotRuntimeConfigKeys() map[string]struct{} {
	return map[string]struct{}{
		// docs/spec/090-configuration.ru.md explicitly says this is not part
		// of the first-version production runtime config contract.
		"TOKENIO_MIGRATIONS_DIR": {},
	}
}

func implementationOnlyEnvKeysPendingSpecReconciliation() map[string]struct{} {
	return map[string]struct{}{
		"TOKENIO_FORWARDING_ATTEMPT_RECOVERY_BATCH_SIZE":      {},
		"TOKENIO_FORWARDING_ATTEMPT_RECOVERY_INTERVAL":        {},
		"TOKENIO_FORWARDING_ATTEMPT_RECOVERY_STALE_AFTER":     {},
		"TOKENIO_HTTP_SHUTDOWN_TIMEOUT":                       {},
		"TOKENIO_TELEGRAM_BALANCE_SCAN_BATCH_SIZE":            {},
		"TOKENIO_TELEGRAM_BALANCE_SCAN_INTERVAL":              {},
		"TOKENIO_TELEGRAM_DELIVERY_BATCH_SIZE":                {},
		"TOKENIO_TELEGRAM_DELIVERY_INTERVAL":                  {},
		"TOKENIO_TELEGRAM_FAILED_RETRY_BATCH_SIZE":            {},
		"TOKENIO_TELEGRAM_FAILED_RETRY_INTERVAL":              {},
		"TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_BATCH_SIZE":  {},
		"TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_INTERVAL":    {},
		"TOKENIO_TELEGRAM_STALE_ATTEMPT_RECOVERY_STALE_AFTER": {},
	}
}

func readAuditFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func collectTokenioEnvKeys(text string) map[string]struct{} {
	matches := regexp.MustCompile(`TOKENIO_[A-Z0-9_]+`).FindAllString(text, -1)
	result := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		result[match] = struct{}{}
	}
	return result
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
