package app

import (
	"fmt"
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/config"
	adminhttp "github.com/bogachenko/tokenio-gateway/internal/transport/http/admin"
	provisioninghttp "github.com/bogachenko/tokenio-gateway/internal/transport/http/provisioning"
	publicapi "github.com/bogachenko/tokenio-gateway/internal/transport/http/publicapi"
	"github.com/bogachenko/tokenio-gateway/internal/transport/httptransport"
)

type TransportGraph struct {
	Public http.Handler
	Admin  http.Handler

	ProvisioningEnabled bool
	Provisioning        http.Handler

	Root http.Handler
}

func NewTransportGraph(
	cfg config.Config,
	primitives RuntimePrimitives,
	security SecurityGraph,
	applications ApplicationGraph,
) (TransportGraph, error) {
	if err := primitives.Validate(); err != nil {
		return TransportGraph{}, fmt.Errorf("validate runtime primitives: %w", err)
	}
	if err := security.Validate(); err != nil {
		return TransportGraph{}, fmt.Errorf("validate security graph: %w", err)
	}
	if err := applications.Validate(); err != nil {
		return TransportGraph{}, fmt.Errorf("validate application graph: %w", err)
	}
	if security.ProvisioningEnabled != applications.ProvisioningEnabled {
		return TransportGraph{}, fmt.Errorf(
			"provisioning security and application capabilities disagree",
		)
	}

	publicRouter, err := publicapi.NewRouter(
		applications.PublicAuthentication,
		applications.ModelCatalog,
		primitives.RequestIDs,
	)
	if err != nil {
		return TransportGraph{}, fmt.Errorf(
			"construct public API HTTP router: %w",
			err,
		)
	}

	adminRouter, err := adminhttp.NewRouter(
		applications.Admin,
		security.AdminAuthenticator,
		primitives.RequestIDs,
	)
	if err != nil {
		return TransportGraph{}, fmt.Errorf("construct admin HTTP router: %w", err)
	}

	var provisioningRouter http.Handler
	if applications.ProvisioningEnabled {
		router, routerErr := provisioninghttp.NewRouter(
			applications.Provisioning,
			security.ProvisioningAuthenticator,
			primitives.RequestIDs,
			cfg.RequestBodyMaxBytes,
		)
		if routerErr != nil {
			return TransportGraph{}, fmt.Errorf(
				"construct provisioning HTTP router: %w",
				routerErr,
			)
		}
		provisioningRouter = router
	}

	rootRouter, err := httptransport.NewRouter(
		publicRouter,
		adminRouter,
		provisioningRouter,
	)
	if err != nil {
		return TransportGraph{}, fmt.Errorf("construct root HTTP router: %w", err)
	}

	graph := TransportGraph{
		Public:              publicRouter,
		Admin:               adminRouter,
		ProvisioningEnabled: applications.ProvisioningEnabled,
		Provisioning:        provisioningRouter,
		Root:                rootRouter,
	}
	if err := graph.Validate(); err != nil {
		return TransportGraph{}, fmt.Errorf("validate transport graph: %w", err)
	}
	return graph, nil
}

func (g TransportGraph) Validate() error {
	switch {
	case g.Public == nil:
		return fmt.Errorf("public API HTTP handler is nil")
	case g.Admin == nil:
		return fmt.Errorf("admin HTTP handler is nil")
	case g.ProvisioningEnabled && g.Provisioning == nil:
		return fmt.Errorf("enabled provisioning HTTP handler is nil")
	case !g.ProvisioningEnabled && g.Provisioning != nil:
		return fmt.Errorf("disabled provisioning HTTP handler is non-nil")
	case g.Root == nil:
		return fmt.Errorf("root HTTP handler is nil")
	default:
		return nil
	}
}
