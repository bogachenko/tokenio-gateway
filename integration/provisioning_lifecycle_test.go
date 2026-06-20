//go:build integration

package integration_test

import (
	"testing"
)

func TestProvisioningLifecycleRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/provisioning_lifecycle_test.go")

	assertScenarioEvidence(t, files, "provisioning endpoint", []string{
		"/internal/v1/api-keys/provision",
		"provision",
		"Provision",
	})
	assertScenarioEvidence(t, files, "provisioning service authentication", []string{
		"X-Service-Token",
		"service token",
		"service_token",
		"ServiceToken",
	})
	assertScenarioEvidence(t, files, "provisioning idempotency", []string{
		"idempotency",
		"idempotency_key",
		"Idempotency",
	})
	assertScenarioEvidence(t, files, "provisioned API key creation", []string{
		"api_key",
		"API key",
		"hash",
		"Hash",
	})
	assertScenarioEvidence(t, files, "provisioning duplicate handling", []string{
		"duplicate",
		"already exists",
		"existing",
		"conflict",
	})
	assertScenarioEvidence(t, files, "provisioning lifecycle automated coverage", []string{
		"TestProvision",
		"provisioning",
		"provision",
	})
}
