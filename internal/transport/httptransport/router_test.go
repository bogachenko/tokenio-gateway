package httptransport

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type rootRouterHandler struct {
	calls int
}

func (h *rootRouterHandler) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	h.calls++
	w.WriteHeader(http.StatusNoContent)
}

func TestNewRouterContract(t *testing.T) {
	router, err := NewRouter(nil, nil, nil, nil)
	if router != nil || !errors.Is(err, ErrInvalidRouterConfig) {
		t.Fatalf("router = %v, error = %v", router, err)
	}

	router, err = NewRouter(
		&rootRouterHandler{},
		&rootRouterHandler{},
		&rootRouterHandler{},
		nil,
	)
	if err != nil || router == nil {
		t.Fatalf("disabled provisioning router: %v", err)
	}
}

func TestRouterHealthAndErrors(t *testing.T) {
	tests := []struct {
		method      string
		path        string
		wantStatus  int
		wantType    string
		wantBody    string
		wantCode    domain.ErrorCode
		wantMessage string
		wantAllow   string
	}{
		{
			method:     http.MethodGet,
			path:       "/health",
			wantStatus: http.StatusOK,
			wantType:   "text/plain",
			wantBody:   "OK",
		},
		{
			method:      http.MethodPost,
			path:        "/health",
			wantStatus:  http.StatusMethodNotAllowed,
			wantType:    "application/json",
			wantCode:    domain.ErrorCodeMethodNotAllowed,
			wantMessage: "Method not allowed",
			wantAllow:   http.MethodGet,
		},
		{
			method:      http.MethodGet,
			path:        "/v1/unknown",
			wantStatus:  http.StatusNotFound,
			wantType:    "application/json",
			wantCode:    domain.ErrorCodeNotFound,
			wantMessage: "Endpoint not found",
		},
	}

	router, err := NewRouter(
		&rootRouterHandler{},
		&rootRouterHandler{},
		&rootRouterHandler{},
		nil,
	)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	for _, test := range tests {
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(test.method, test.path, nil))
		if recorder.Code != test.wantStatus {
			t.Fatalf("path %s: status = %d, want %d", test.path, recorder.Code, test.wantStatus)
		}
		if recorder.Header().Get("Content-Type") != test.wantType {
			t.Fatalf("path %s: Content-Type = %q", test.path, recorder.Header().Get("Content-Type"))
		}
		if recorder.Header().Get("Allow") != test.wantAllow {
			t.Fatalf("path %s: Allow = %q", test.path, recorder.Header().Get("Allow"))
		}
		if test.wantBody != "" {
			if recorder.Body.String() != test.wantBody {
				t.Fatalf("path %s: body = %q", test.path, recorder.Body.String())
			}
			continue
		}
		var response ErrorResponse
		if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
			t.Fatalf("decode error response: %v", err)
		}
		if response.Error.Code != test.wantCode || response.Error.Message != test.wantMessage {
			t.Fatalf("path %s: error = %#v", test.path, response.Error)
		}
	}
}

func TestRouterDispatchesOnlyExactBoundaries(t *testing.T) {
	tests := []struct {
		path             string
		wantStatus       int
		wantPublicCalls  int
		wantLLMCalls     int
		wantAdminCalls   int
		wantProvisioning int
	}{
		{path: "/v1/models", wantStatus: http.StatusNoContent, wantPublicCalls: 1},
		{path: "/v1/models/", wantStatus: http.StatusNotFound},
		{path: "/v1/modelsevil", wantStatus: http.StatusNotFound},
		{path: "/v1/chat/completions", wantStatus: http.StatusNoContent, wantLLMCalls: 1},
		{path: "/v1/embeddings", wantStatus: http.StatusNoContent, wantLLMCalls: 1},
		{path: "/v1/images/generations", wantStatus: http.StatusNoContent, wantLLMCalls: 1},
		{path: "/v1/chat/completions/", wantStatus: http.StatusNotFound},
		{path: "/v1/embeddingsevil", wantStatus: http.StatusNotFound},
		{path: "/admin/v1", wantStatus: http.StatusNoContent, wantAdminCalls: 1},
		{path: "/admin/v1/users", wantStatus: http.StatusNoContent, wantAdminCalls: 1},
		{path: "/admin/v1evil", wantStatus: http.StatusNotFound},
		{path: "/internal/v1/api-key-provisionings", wantStatus: http.StatusNoContent, wantProvisioning: 1},
		{path: "/internal/v1/api-key-provisionings/prov_1/confirm-delivery", wantStatus: http.StatusNoContent, wantProvisioning: 1},
		{path: "/internal/v1/api-key-provisioningsevil", wantStatus: http.StatusNotFound},
		{path: "/internal/v1", wantStatus: http.StatusNotFound},
		{path: "/v1/users", wantStatus: http.StatusNotFound},
	}

	for _, test := range tests {
		public := &rootRouterHandler{}
		llm := &rootRouterHandler{}
		admin := &rootRouterHandler{}
		provisioning := &rootRouterHandler{}
		router, err := NewRouter(
			public,
			llm,
			admin,
			provisioning,
		)
		if err != nil {
			t.Fatalf("NewRouter: %v", err)
		}
		recorder := httptest.NewRecorder()
		router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, test.path, nil))
		if recorder.Code != test.wantStatus {
			t.Fatalf("path %s: status = %d, want %d", test.path, recorder.Code, test.wantStatus)
		}
		if public.calls != test.wantPublicCalls ||
			llm.calls != test.wantLLMCalls ||
			admin.calls != test.wantAdminCalls ||
			provisioning.calls != test.wantProvisioning {
			t.Fatalf(
				"path %s: public=%d llm=%d admin=%d provisioning=%d",
				test.path,
				public.calls,
				llm.calls,
				admin.calls,
				provisioning.calls,
			)
		}
	}
}

func TestRouterDoesNotDispatchDisabledProvisioning(t *testing.T) {
	router, err := NewRouter(
		&rootRouterHandler{},
		&rootRouterHandler{},
		&rootRouterHandler{},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	recorder := httptest.NewRecorder()
	router.ServeHTTP(
		recorder,
		httptest.NewRequest(http.MethodPost, "/internal/v1/api-key-provisionings", nil),
	)
	if recorder.Code != http.StatusNotFound {
		t.Fatalf("status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
}

func TestRouterRoutesAnthropicMessagesAsPublicLLMPath(t *testing.T) {
	public := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("public handler must not receive Anthropic messages")
	})
	admin := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("admin handler must not receive Anthropic messages")
	})
	llm := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		w.WriteHeader(http.StatusAccepted)
	})
	router, err := NewRouter(public, llm, admin, nil)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(
		response,
		httptest.NewRequest(http.MethodPost, "/v1/messages", nil),
	)

	if response.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestRouterRoutesGeminiNativeModelOperationsToLLM(t *testing.T) {
	llmCalled := false
	router, err := NewRouter(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("public handler called for %s", r.URL.Path)
		}),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			llmCalled = true
			if r.URL.Path != "/v1beta/models/gemini-1.5-pro:generateContent" {
				t.Fatalf("path=%s", r.URL.Path)
			}
			w.WriteHeader(http.StatusAccepted)
		}),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("admin handler called for %s", r.URL.Path)
		}),
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	router.ServeHTTP(
		response,
		httptest.NewRequest(
			http.MethodPost,
			"/v1beta/models/gemini-1.5-pro:generateContent",
			nil,
		),
	)

	if response.Code != http.StatusAccepted || !llmCalled {
		t.Fatalf("status=%d llmCalled=%v body=%s", response.Code, llmCalled, response.Body.String())
	}
}
