package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestListModelCatalogRoutesRejectsInvalidAPIFamilyBeforeDB(
	t *testing.T,
) {
	repository := &RouteRepository{}

	routes, err := repository.ListModelCatalogRoutes(
		context.Background(),
		domain.APIFamily("unknown"),
	)
	if routes != nil {
		t.Fatalf("routes = %+v, want nil", routes)
	}
	if !errors.Is(
		err,
		ports.ErrStoreContractViolation,
	) {
		t.Fatalf(
			"error = %v, want store contract violation",
			err,
		)
	}
}

func TestModelCatalogRouteSQLLoadsUnavailableRoutes(
	t *testing.T,
) {
	lower := strings.ToLower(listModelCatalogRoutesSQL)

	for _, required := range []string{
		"where api_family = $1",
		"order by",
		"client_model asc",
		"endpoint_kind asc",
		"priority asc",
		"id asc",
	} {
		if !strings.Contains(lower, required) {
			t.Fatalf(
				"catalog SQL is missing %q: %s",
				required,
				listModelCatalogRoutesSQL,
			)
		}
	}

	for _, forbidden := range []string{
		"enabled = true",
		"cooldown_until <",
		"cooldown_until is null",
	} {
		if strings.Contains(lower, forbidden) {
			t.Fatalf(
				"catalog SQL prematurely filters %q: %s",
				forbidden,
				listModelCatalogRoutesSQL,
			)
		}
	}
}
