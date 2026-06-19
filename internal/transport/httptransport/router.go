package httptransport

import (
	"errors"
	"net/http"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

const provisioningBasePath = "/internal/v1/api-key-provisionings"

var ErrInvalidRouterConfig = errors.New("invalid HTTP router config")

type Router struct {
	public       http.Handler
	llm          http.Handler
	admin        http.Handler
	provisioning http.Handler
}

func NewRouter(
	public http.Handler,
	llm http.Handler,
	admin http.Handler,
	provisioning http.Handler,
) (*Router, error) {
	if public == nil || llm == nil || admin == nil {
		return nil, ErrInvalidRouterConfig
	}
	return &Router{
		public:       public,
		llm:          llm,
		admin:        admin,
		provisioning: provisioning,
	}, nil
}

func (h *Router) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.URL.Path == HealthPath ||
		r.URL.Path == ReadinessPath ||
		r.URL.Path == "/health":
		HealthHandler(w, r)
	case r.URL.Path == "/v1/models":
		h.public.ServeHTTP(w, r)
	case r.URL.Path == "/api/tags":
		h.public.ServeHTTP(w, r)
	case isPublicLLMPath(r.URL.Path):
		h.llm.ServeHTTP(w, r)
	case r.URL.Path == "/admin/v1" || strings.HasPrefix(r.URL.Path, "/admin/v1/"):
		h.admin.ServeHTTP(w, r)
	case h.provisioning != nil &&
		(r.URL.Path == provisioningBasePath || strings.HasPrefix(r.URL.Path, provisioningBasePath+"/")):
		h.provisioning.ServeHTTP(w, r)
	default:
		WriteGatewayError(w, GatewayError{
			Status:  http.StatusNotFound,
			Code:    domain.ErrorCodeNotFound,
			Message: "Endpoint not found",
		})
	}
}

func isPublicLLMPath(path string) bool {
	switch path {
	case "/v1/chat/completions",
		"/v1/embeddings",
		"/v1/images/generations",
		"/v1/messages",
		"/api/chat",
		"/api/generate",
		"/api/embeddings":
		return true
	default:
		return strings.HasPrefix(path, "/v1beta/models/")
	}
}
