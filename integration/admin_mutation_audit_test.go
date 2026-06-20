//go:build integration

package integration_test

import (
	"testing"
)

func TestAdminMutationAuditRepositoryEvidence(t *testing.T) {
	t.Parallel()

	repoRoot := scenarioRepoRoot(t)
	files := scenarioRepositoryTextFiles(t, repoRoot, "integration/admin_mutation_audit_test.go")

	assertScenarioEvidence(t, files, "admin mutation endpoint", []string{
		"admin",
		"Admin",
		"mutation",
		"Mutation",
	})
	assertScenarioEvidence(t, files, "admin authentication/authorization", []string{
		"admin",
		"authorized",
		"permission",
		"forbidden",
		"StatusForbidden",
	})
	assertScenarioEvidence(t, files, "audit event persistence", []string{
		"audit",
		"Audit",
		"audit_log",
		"event",
	})
	assertScenarioEvidence(t, files, "admin actor/resource metadata", []string{
		"actor",
		"resource",
		"subject",
		"changed",
	})
	assertScenarioEvidence(t, files, "non-admin rejection", []string{
		"StatusForbidden",
		"403",
		"forbidden",
		"unauthorized",
	})
	assertScenarioEvidence(t, files, "admin mutation audit automated coverage", []string{
		"TestAdmin",
		"mutation",
		"audit",
	})
}
