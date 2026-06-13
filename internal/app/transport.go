package app

import (
	"fmt"
	"net/http"

	adminhttp "github.com/bogachenko/tokenio-gateway/internal/transport/http/admin"
	"github.com/bogachenko/tokenio-gateway/internal/transport/httptransport"
)

type TransportGraph struct {
	Admin http.Handler
	Root  http.Handler
}

func NewTransportGraph(
	primitives RuntimePrimitives,
	security SecurityGraph,
	applications ApplicationGraph,
) (TransportGraph, error) {
	if err := primitives.Validate(); err != nil {
		return TransportGraph{}, fmt.Errorf(
			"validate runtime primitives: %w",
			err,
		)
	}
	if err := security.Validate(); err != nil {
		return TransportGraph{}, fmt.Errorf(
			"validate security graph: %w",
			err,
		)
	}
	if err := applications.Validate(); err != nil {
		return TransportGraph{}, fmt.Errorf(
			"validate application graph: %w",
			err,
		)
	}

	adminRouter, err := adminhttp.NewRouter(
		applications.Admin,
		security.AdminAuthenticator,
		primitives.RequestIDs,
	)
	if err != nil {
		return TransportGraph{}, fmt.Errorf(
			"construct admin HTTP router: %w",
			err,
		)
	}

	rootRouter, err := httptransport.NewRouter(adminRouter)
	if err != nil {
		return TransportGraph{}, fmt.Errorf(
			"construct root HTTP router: %w",
			err,
		)
	}

	graph := TransportGraph{
		Admin: adminRouter,
		Root:  rootRouter,
	}
	if err := graph.Validate(); err != nil {
		return TransportGraph{}, fmt.Errorf(
			"validate transport graph: %w",
			err,
		)
	}
	return graph, nil
}

func (g TransportGraph) Validate() error {
	switch {
	case g.Admin == nil:
		return fmt.Errorf("admin HTTP handler is nil")
	case g.Root == nil:
		return fmt.Errorf("root HTTP handler is nil")
	default:
		return nil
	}
}
