package httptransport

import (
	"errors"
	"net/http"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

var ErrInvalidRouterConfig = errors.New("invalid HTTP router config")

type Router struct {
	admin http.Handler
}

func NewRouter(admin http.Handler) (*Router, error) {
	if admin == nil {
		return nil, ErrInvalidRouterConfig
	}
	return &Router{admin: admin}, nil
}

func (h *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == "/health":
		healthHandler(w, r)
	case r.URL.Path == "/admin/v1" ||
		strings.HasPrefix(r.URL.Path, "/admin/v1/"):
		h.admin.ServeHTTP(w, r)
	default:
		WriteGatewayError(w, GatewayError{
			Status:  http.StatusNotFound,
			Code:    domain.ErrorCodeNotFound,
			Message: "Not found",
		})
	}
}
