package openaicompat

import (
	"context"
	"fmt"
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type Client struct {
	adapter ports.ForwardingAdapter
}

var _ ports.ForwardingClient = (*Client)(nil)

func newClient(adapter ports.ForwardingAdapter) (*Client, error) {
	if adapter == nil {
		return nil, ErrInvalidAdapterConfig
	}
	return &Client{adapter: adapter}, nil
}

func (c *Client) Forward(
	ctx context.Context,
	request ports.ForwardingClientRequest,
) (ports.ForwardResponse, error) {
	if c == nil || c.adapter == nil {
		return ports.ForwardResponse{}, ErrInvalidAdapterConfig
	}
	path, ok := endpointPath(request.Route.EndpointKind)
	if !ok {
		return ports.ForwardResponse{}, ErrUnsupportedRoute
	}
	response, err := c.adapter.Forward(
		ctx,
		ports.ForwardRequest{
			Route:  request.Route,
			Method: http.MethodPost,
			Path:   path,
			Headers: map[string][]string{
				"Content-Type": {"application/json"},
			},
			Body: append([]byte(nil), request.Body...),
		},
	)
	if err != nil {
		return ports.ForwardResponse{}, fmt.Errorf(
			"forward openai-compatible request: %w",
			err,
		)
	}
	return response, nil
}
