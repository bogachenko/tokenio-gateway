package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTransportGraphPublishesCompletePublicLLMAPI(
	t *testing.T,
) {
	cfg,
		_,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories := validApplicationGraphInputs(t)

	graph := buildTransportGraph(
		t,
		cfg,
		security,
		provisioningInfrastructure,
		billingInfrastructure,
		forwardingInfrastructure,
		repositories,
	)

	health := httptest.NewRecorder()
	graph.Root.ServeHTTP(
		health,
		httptest.NewRequest(
			http.MethodGet,
			"/health",
			nil,
		),
	)
	if health.Code != http.StatusOK {
		t.Fatalf(
			"health status=%d body=%s",
			health.Code,
			health.Body.String(),
		)
	}

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{
			name:   "models",
			method: http.MethodGet,
			path:   "/v1/models",
		},
		{
			name:   "chat completions",
			method: http.MethodPost,
			path:   "/v1/chat/completions",
		},
		{
			name:   "embeddings",
			method: http.MethodPost,
			path:   "/v1/embeddings",
		},
		{
			name:   "image generations",
			method: http.MethodPost,
			path:   "/v1/images/generations",
		},
		{
			name:   "anthropic messages",
			method: http.MethodPost,
			path:   "/v1/messages",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := httptest.NewRecorder()
			graph.Root.ServeHTTP(
				response,
				httptest.NewRequest(
					test.method,
					test.path,
					nil,
				),
			)

			if response.Code != http.StatusUnauthorized ||
				!strings.Contains(
					response.Body.String(),
					`"code":"unauthorized"`,
				) ||
				!strings.Contains(
					response.Body.String(),
					`"request_id":"llmreq_`,
				) ||
				!strings.HasPrefix(
					response.Header().Get(
						"X-Local-Request-ID",
					),
					"llmreq_",
				) {
				t.Fatalf(
					"path=%s status=%d headers=%v body=%s",
					test.path,
					response.Code,
					response.Header(),
					response.Body.String(),
				)
			}
		})
	}

	notFound := httptest.NewRecorder()
	graph.Root.ServeHTTP(
		notFound,
		httptest.NewRequest(
			http.MethodPost,
			"/v1/unknown",
			nil,
		),
	)
	if notFound.Code != http.StatusNotFound ||
		!strings.Contains(
			notFound.Body.String(),
			`"code":"not_found"`,
		) {
		t.Fatalf(
			"unknown status=%d body=%s",
			notFound.Code,
			notFound.Body.String(),
		)
	}
}
