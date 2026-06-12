package httptransport

import (
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func healthHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		WriteGatewayError(w, GatewayError{
			Status:  http.StatusMethodNotAllowed,
			Code:    domain.ErrorCodeMethodNotAllowed,
			Message: "Method not allowed",
		})
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}
