package httptransport

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRouterRoutesGeminiNativeModelCatalogToPublicHandler(t *testing.T) {
	publicCalled := false
	router, err := NewRouter(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			publicCalled = true
			if r.URL.Path != "/v1beta/models" {
				t.Fatalf("path=%s", r.URL.Path)
			}
			w.WriteHeader(http.StatusAccepted)
		}),
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			t.Fatalf("LLM handler called for %s", r.URL.Path)
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
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1beta/models", nil))

	if response.Code != http.StatusAccepted || !publicCalled {
		t.Fatalf("status=%d publicCalled=%v body=%s", response.Code, publicCalled, response.Body.String())
	}
}

func TestRouterKeepsGeminiNativeModelOperationsOnLLMHandler(t *testing.T) {
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
	router.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1beta/models/gemini-1.5-pro:generateContent", nil))

	if response.Code != http.StatusAccepted || !llmCalled {
		t.Fatalf("status=%d llmCalled=%v body=%s", response.Code, llmCalled, response.Body.String())
	}
}
