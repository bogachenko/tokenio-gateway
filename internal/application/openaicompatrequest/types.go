package openaicompatrequest

import (
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type ParsedRequest struct {
	Body                  []byte
	Query                 ports.RouteQuery
	RequestedCapabilities domain.CapabilitySet
}
