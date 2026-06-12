package httptransport

import (
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func NewRouter() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			healthHandler(w, r)
		default:
			WriteGatewayError(w, GatewayError{
				Status:  http.StatusNotFound,
				Code:    domain.ErrorCodeNotFound,
				Message: "Not found",
			})
		}
	})
}
