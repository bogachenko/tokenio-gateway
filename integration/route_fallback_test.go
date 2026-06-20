//go:build integration

package integration_test

import (
	"testing"
)

func TestRouteFallbackRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/route_fallback_test.go")

	assertScenarioEvidence(t, files, "route fallback decision/policy", []string{
		"fallback",
		"Fallback",
	})
	assertScenarioEvidence(t, files, "retryable provider failure classification", []string{
		"retryable",
		"Retryable",
		"temporary",
		"Temporary",
	})
	assertScenarioEvidence(t, files, "provider attempt/order evidence", []string{
		"attempt",
		"Attempt",
		"route",
		"Route",
	})
	assertScenarioEvidence(t, files, "terminal no-route/no-capacity failure", []string{
		"no_route",
		"no route",
		"NoRoute",
		"capacity",
	})
	assertScenarioEvidence(t, files, "route fallback automated coverage", []string{
		"TestRoute",
		"TestFallback",
		"fallback",
	})
}
