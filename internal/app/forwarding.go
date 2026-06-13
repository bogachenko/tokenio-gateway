package app

import (
	"fmt"

	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/forwarding/openaicompat"
	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/forwarding/rewritesupport"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type ForwardingInfrastructureGraph struct {
	ModelRewriteSupport ports.ModelIdentifierRewriteSupport
}

func NewForwardingInfrastructureGraph() (ForwardingInfrastructureGraph, error) {
	registry, err := rewritesupport.NewRegistry(
		openaicompat.NewModelRewriteSupport(),
	)
	if err != nil {
		return ForwardingInfrastructureGraph{},
			fmt.Errorf(
				"construct model rewrite support registry: %w",
				err,
			)
	}

	graph := ForwardingInfrastructureGraph{
		ModelRewriteSupport: registry,
	}
	if err := graph.Validate(); err != nil {
		return ForwardingInfrastructureGraph{},
			fmt.Errorf(
				"validate forwarding infrastructure graph: %w",
				err,
			)
	}
	return graph, nil
}

func (g ForwardingInfrastructureGraph) Validate() error {
	if g.ModelRewriteSupport == nil {
		return fmt.Errorf(
			"model rewrite support registry is nil",
		)
	}
	return nil
}
