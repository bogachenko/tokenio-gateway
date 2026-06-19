package gemininative

import (
	"context"
	"net/http"
	"net/url"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
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
	path, err := forwardingPath(request.Route)
	if err != nil {
		return ports.ForwardResponse{}, err
	}
	return c.adapter.Forward(
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
}

func forwardingPath(route domain.Route) (string, error) {
	if route.APIFamily != domain.APIFamilyGeminiNative {
		return "", ErrUnsupportedRoute
	}
	model := route.ClientModel
	if model == "" {
		return "", ErrUnsupportedRoute
	}
	operation := ""
	switch route.EndpointKind {
	case domain.EndpointChat:
		operation = "generateContent"
	case domain.EndpointEmbeddings:
		operation = "embedContent"
	default:
		return "", ErrUnsupportedRoute
	}
	return "/v1beta/models/" + url.PathEscape(model) + ":" + operation, nil
}
