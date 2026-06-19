//go:build integration

package integration_test

import (
	"io"
	"net/http"
	"strings"
	"testing"

	fa "github.com/bogachenko/tokenio-gateway/integration/fakes/anthropic"
	fb "github.com/bogachenko/tokenio-gateway/integration/fakes/billing"
	fg "github.com/bogachenko/tokenio-gateway/integration/fakes/gemini"
	fo "github.com/bogachenko/tokenio-gateway/integration/fakes/ollama"
	foc "github.com/bogachenko/tokenio-gateway/integration/fakes/openaicompat"
	ft "github.com/bogachenko/tokenio-gateway/integration/fakes/telegram"
)

func TestFakeServicesAuthenticationFailureScenario(t *testing.T) {
	t.Parallel()

	t.Run("billing", func(t *testing.T) {
		server := fb.New()
		defer server.Close()

		server.SetResponse(fb.Response{
			Status: http.StatusUnauthorized,
			Header: authJSONHeader(),
			Body:   []byte(`{"error":"unauthorized"}`),
		})
		response, err := http.Post(server.URL()+"/v1/charges", "application/json", strings.NewReader(`{}`))
		assertAuthFailure(t, response, err, http.StatusUnauthorized, "unauthorized")
	})

	t.Run("openai compatible", func(t *testing.T) {
		server := foc.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/v1/chat/completions", foc.Response{
			Status: http.StatusUnauthorized,
			Header: authJSONHeader(),
			Body:   []byte(`{"error":{"message":"unauthorized","type":"invalid_request_error"}}`),
		})
		response, err := http.Post(server.URL()+"/v1/chat/completions", "application/json", strings.NewReader(`{}`))
		assertAuthFailure(t, response, err, http.StatusUnauthorized, "unauthorized")
	})

	t.Run("anthropic", func(t *testing.T) {
		server := fa.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/v1/messages", fa.Response{
			Status: http.StatusUnauthorized,
			Header: authJSONHeader(),
			Body:   []byte(`{"type":"error","error":{"type":"authentication_error","message":"unauthorized"}}`),
		})
		response, err := http.Post(server.URL()+"/v1/messages", "application/json", strings.NewReader(`{}`))
		assertAuthFailure(t, response, err, http.StatusUnauthorized, "unauthorized")
	})

	t.Run("gemini", func(t *testing.T) {
		server := fg.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/v1beta/models/gemini-test:generateContent", fg.Response{
			Status: http.StatusUnauthorized,
			Header: authJSONHeader(),
			Body:   []byte(`{"error":{"code":401,"message":"unauthorized","status":"UNAUTHENTICATED"}}`),
		})
		response, err := http.Post(server.URL()+"/v1beta/models/gemini-test:generateContent", "application/json", strings.NewReader(`{}`))
		assertAuthFailure(t, response, err, http.StatusUnauthorized, "unauthorized")
	})

	t.Run("ollama", func(t *testing.T) {
		server := fo.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/api/chat", fo.Response{
			Status: http.StatusUnauthorized,
			Header: authJSONHeader(),
			Body:   []byte(`{"error":"unauthorized"}`),
		})
		response, err := http.Post(server.URL()+"/api/chat", "application/json", strings.NewReader(`{}`))
		assertAuthFailure(t, response, err, http.StatusUnauthorized, "unauthorized")
	})

	t.Run("telegram", func(t *testing.T) {
		server := ft.New()
		defer server.Close()

		server.SetResponse(http.MethodPost, "/botTEST_TOKEN/sendMessage", ft.Response{
			Status: http.StatusUnauthorized,
			Header: authJSONHeader(),
			Body:   []byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`),
		})
		response, err := http.Post(server.BotAPIURL("TEST_TOKEN")+"/sendMessage", "application/json", strings.NewReader(`{}`))
		assertAuthFailure(t, response, err, http.StatusUnauthorized, "Unauthorized")
	})
}

func authJSONHeader() http.Header {
	return http.Header{
		"Content-Type": []string{"application/json"},
	}
}

func assertAuthFailure(t *testing.T, response *http.Response, err error, wantStatus int, wantBodyPart string) {
	t.Helper()

	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read response body: %v", err)
	}
	if response.StatusCode != wantStatus {
		t.Fatalf("status=%d want=%d body=%s", response.StatusCode, wantStatus, string(body))
	}
	if !strings.Contains(string(body), wantBodyPart) {
		t.Fatalf("body=%s does not contain %q", string(body), wantBodyPart)
	}
}
