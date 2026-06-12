package httptransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

var _ PublicAuthenticator = (*authenticate.UseCase)(nil)

type fakeAuthenticator struct {
	result  authenticate.Result
	err     error
	calls   int
	lastRaw string
	lastCtx context.Context
}

func (f *fakeAuthenticator) AuthenticatePublicRequest(ctx context.Context, input authenticate.Input) (authenticate.Result, error) {
	f.calls++
	f.lastRaw = input.RawAPIKey
	f.lastCtx = ctx
	return f.result, f.err
}

type staticRequestIDGenerator string

func (g staticRequestIDGenerator) NewLocalRequestID() string { return string(g) }

func TestNewPublicAuthMiddlewareRejectsNilDependencies(t *testing.T) {
	if _, err := NewPublicAuthMiddleware(nil, staticRequestIDGenerator("llmreq_1")); err == nil {
		t.Fatal("expected nil authenticator error")
	}
	if _, err := NewPublicAuthMiddleware(&fakeAuthenticator{}, nil); err == nil {
		t.Fatal("expected nil generator error")
	}
}

func TestPublicAuthMiddlewareFailures(t *testing.T) {
	secretErr := errors.New("repository failed with raw secret sk_secret_value")
	tests := []struct {
		name          string
		authorization string
		authErr       error
		wantStatus    int
		wantCode      domain.ErrorCode
		wantMessage   string
		wantCalls     int
		mustNotLeak   string
	}{
		{name: "missing Authorization", wantStatus: http.StatusUnauthorized, wantCode: domain.ErrorCodeUnauthorized, wantMessage: "Authorization header is required"},
		{name: "wrong scheme", authorization: "Basic sk_test", wantStatus: http.StatusUnauthorized, wantCode: domain.ErrorCodeUnauthorized, wantMessage: "Authorization header format must be Bearer {api_key}"},
		{name: "empty Bearer value", authorization: "Bearer", wantStatus: http.StatusUnauthorized, wantCode: domain.ErrorCodeUnauthorized, wantMessage: "Bearer API key is empty"},
		{name: "wrong sk prefix", authorization: "Bearer pk_test", wantStatus: http.StatusUnauthorized, wantCode: domain.ErrorCodeUnauthorized, wantMessage: "API key must start with sk_"},
		{name: "unknown key", authorization: "Bearer sk_unknown", authErr: authenticate.ErrInvalidAPIKey, wantStatus: http.StatusUnauthorized, wantCode: domain.ErrorCodeInvalidAPIKey, wantMessage: "Invalid API key", wantCalls: 1},
		{name: "disabled key", authorization: "Bearer sk_disabled", authErr: fmt.Errorf("wrapped: %w", authenticate.ErrInvalidAPIKey), wantStatus: http.StatusUnauthorized, wantCode: domain.ErrorCodeInvalidAPIKey, wantMessage: "Invalid API key", wantCalls: 1},
		{name: "disabled user", authorization: "Bearer sk_disabled_user", authErr: authenticate.ErrUserDisabled, wantStatus: http.StatusForbidden, wantCode: domain.ErrorCodeUserDisabled, wantMessage: "User is disabled", wantCalls: 1},
		{name: "invalid billing identity", authorization: "Bearer sk_invalid_identity", authErr: authenticate.ErrInvalidIdentity, wantStatus: http.StatusInternalServerError, wantCode: domain.ErrorCodeInternalError, wantMessage: "Internal server error", wantCalls: 1},
		{name: "repository error", authorization: "Bearer sk_secret_value", authErr: secretErr, wantStatus: http.StatusInternalServerError, wantCode: domain.ErrorCodeInternalError, wantMessage: "Internal server error", wantCalls: 1, mustNotLeak: "sk_secret_value"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &fakeAuthenticator{err: tt.authErr}
			middleware, err := NewPublicAuthMiddleware(fake, staticRequestIDGenerator("llmreq_test"))
			if err != nil {
				t.Fatal(err)
			}
			nextCalled := false
			handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				nextCalled = true
			}))

			request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
			if tt.authorization != "" {
				request.Header.Set("Authorization", tt.authorization)
			}
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			if nextCalled {
				t.Fatal("next handler called on auth failure")
			}
			if fake.calls != tt.wantCalls {
				t.Fatalf("authenticator calls = %d, want %d", fake.calls, tt.wantCalls)
			}
			if recorder.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body %s", recorder.Code, tt.wantStatus, recorder.Body.String())
			}
			if got := recorder.Header().Get("X-Local-Request-ID"); got != "llmreq_test" {
				t.Fatalf("X-Local-Request-ID = %q", got)
			}
			var response ErrorResponse
			if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
				t.Fatal(err)
			}
			if response.Error.Code != tt.wantCode || response.Error.Message != tt.wantMessage || response.Error.RequestID != "llmreq_test" {
				t.Fatalf("error = %#v", response.Error)
			}
			if tt.mustNotLeak != "" && strings.Contains(recorder.Body.String(), tt.mustNotLeak) {
				t.Fatalf("response leaked %q: %s", tt.mustNotLeak, recorder.Body.String())
			}
		})
	}
}

func TestPublicAuthMiddlewareSuccess(t *testing.T) {
	rawAPIKey := "sk_live_secret"
	principal := auth.APIKeyPrincipal{UserID: "user_1", APIKeyID: "key_1", BillingSubjectUserID: "billing_1"}
	fake := &fakeAuthenticator{result: authenticate.Result{Principal: principal}}
	middleware, err := NewPublicAuthMiddleware(fake, staticRequestIDGenerator("llmreq_success"))
	if err != nil {
		t.Fatal(err)
	}

	var downstreamAuthorization string
	var downstreamRequestID string
	var downstreamPrincipal auth.APIKeyPrincipal
	var downstreamPrincipalOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		downstreamAuthorization = r.Header.Get("Authorization")
		downstreamRequestID, _ = RequestIDFromContext(r.Context())
		downstreamPrincipal, downstreamPrincipalOK = PrincipalFromContext(r.Context())
		_, _ = w.Write([]byte("principal propagated"))
	})

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer "+rawAPIKey)
	request.Header.Set("X-Local-Request-ID", "client_supplied")
	recorder := httptest.NewRecorder()
	middleware.Wrap(next).ServeHTTP(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", recorder.Code, recorder.Body.String())
	}
	if fake.calls != 1 || fake.lastRaw != rawAPIKey {
		t.Fatalf("authenticator calls/raw = %d/%q", fake.calls, fake.lastRaw)
	}
	if got, _ := RequestIDFromContext(fake.lastCtx); got != "llmreq_success" {
		t.Fatalf("authenticator context request id = %q", got)
	}
	if got := recorder.Header().Get("X-Local-Request-ID"); got != downstreamRequestID || got != "llmreq_success" {
		t.Fatalf("request id header/context = %q/%q", got, downstreamRequestID)
	}
	if downstreamAuthorization != "" {
		t.Fatalf("downstream Authorization = %q, want absent", downstreamAuthorization)
	}
	if !downstreamPrincipalOK || downstreamPrincipal != principal {
		t.Fatalf("principal = %#v ok %v", downstreamPrincipal, downstreamPrincipalOK)
	}
	if strings.Contains(recorder.Body.String(), rawAPIKey) {
		t.Fatalf("response leaked raw api key: %s", recorder.Body.String())
	}
}

func TestPublicAuthMiddlewareEmptyGeneratedRequestID(t *testing.T) {
	fake := &fakeAuthenticator{}
	middleware, err := NewPublicAuthMiddleware(fake, staticRequestIDGenerator(""))
	if err != nil {
		t.Fatal(err)
	}
	nextCalled := false
	handler := middleware.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		nextCalled = true
	}))

	request := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	request.Header.Set("Authorization", "Bearer sk_live")
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)

	if nextCalled {
		t.Fatal("next handler called")
	}
	if fake.calls != 0 {
		t.Fatalf("authenticator calls = %d", fake.calls)
	}
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d", recorder.Code)
	}
	var response ErrorResponse
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != domain.ErrorCodeInternalError || response.Error.Message != "Internal server error" || response.Error.RequestID != "" {
		t.Fatalf("error = %#v", response.Error)
	}
}
