package publicapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	modelcatalogapp "github.com/bogachenko/tokenio-gateway/internal/application/modelcatalog"
	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type testAuthentication struct {
	result authenticateapp.Result
	err    error
	calls  int
	rawKey string
}

func (a *testAuthentication) AuthenticatePublicRequest(
	_ context.Context,
	input authenticateapp.Input,
) (authenticateapp.Result, error) {
	a.calls++
	a.rawKey = input.RawAPIKey
	return a.result, a.err
}

type testModelCatalog struct {
	result modelcatalogapp.Catalog
	err    error
	calls  int
	family domain.APIFamily
}

func (m *testModelCatalog) List(
	_ context.Context,
	family domain.APIFamily,
) (modelcatalogapp.Catalog, error) {
	m.calls++
	m.family = family
	return m.result, m.err
}

type testRequestIDs struct {
	local string
	err   error
	calls int
}

func (i *testRequestIDs) NewLocalRequestID() (string, error) {
	i.calls++
	return i.local, i.err
}

func (*testRequestIDs) NewAdminRequestID() (string, error) {
	return "", errors.New("unexpected admin request ID")
}

func (*testRequestIDs) NewProvisioningRequestID() (string, error) {
	return "", errors.New(
		"unexpected provisioning request ID",
	)
}

func successfulAuthentication() *testAuthentication {
	return &testAuthentication{
		result: authenticateapp.Result{
			Principal: auth.APIKeyPrincipal{
				UserID:               "usr_1",
				APIKeyID:             "key_1",
				BillingSubjectUserID: "billing_1",
			},
		},
	}
}

func TestModelsEndpointAuthenticatesAndReturnsSafeCatalog(
	t *testing.T,
) {
	authentication := successfulAuthentication()
	models := &testModelCatalog{
		result: modelcatalogapp.Catalog{
			Object: "list",
			Data: []modelcatalogapp.Model{
				{
					ID:      "gpt-test",
					Object:  "model",
					OwnedBy: "tokenio",
					Type:    "chat",
					Active:  true,
					Pricing: &modelcatalogapp.Pricing{
						Currency:                    "RUB",
						InputPricePer1MTokensCents:  100,
						OutputPricePer1MTokensCents: 200,
					},
					Capabilities: domain.CapabilitySet{
						Chat:  true,
						Tools: true,
					},
				},
			},
		},
	}
	ids := &testRequestIDs{
		local: "llmreq_models_1",
	}
	router, err := NewRouter(
		authentication,
		models,
		ids,
	)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		modelsPath,
		nil,
	)
	request.Header.Set(
		"Authorization",
		"Bearer sk_live_public_test",
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf(
			"status=%d body=%s",
			response.Code,
			response.Body.String(),
		)
	}
	if response.Header().Get(
		localRequestIDHeader,
	) != "llmreq_models_1" {
		t.Fatalf(
			"request id header = %q",
			response.Header().Get(
				localRequestIDHeader,
			),
		)
	}
	if response.Header().Get(
		"Content-Type",
	) != "application/json" {
		t.Fatalf(
			"Content-Type = %q",
			response.Header().Get("Content-Type"),
		)
	}
	if authentication.calls != 1 ||
		authentication.rawKey !=
			"sk_live_public_test" ||
		models.calls != 1 ||
		models.family !=
			domain.APIFamilyOpenAICompatible ||
		ids.calls != 1 {
		t.Fatalf(
			"auth calls=%d raw=%q models calls=%d "+
				"family=%q id calls=%d",
			authentication.calls,
			authentication.rawKey,
			models.calls,
			models.family,
			ids.calls,
		)
	}

	var catalog modelcatalogapp.Catalog
	if err := json.Unmarshal(
		response.Body.Bytes(),
		&catalog,
	); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if catalog.Object != "list" ||
		len(catalog.Data) != 1 ||
		catalog.Data[0].ID != "gpt-test" {
		t.Fatalf("catalog = %+v", catalog)
	}

	body := response.Body.String()
	for _, forbidden := range []string{
		"route_id",
		"reseller_id",
		"provider_model",
		"api_key_env",
		"markup_coefficient",
		"sk_live_public_test",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf(
				"response leaked %q: %s",
				forbidden,
				body,
			)
		}
	}
}

func TestOllamaTagsEndpointAuthenticatesAndReturnsOllamaCatalog(t *testing.T) {
	authentication := successfulAuthentication()
	models := &testModelCatalog{
		result: modelcatalogapp.Catalog{
			Object: "list",
			Data: []modelcatalogapp.Model{
				{
					ID:      "llama3.1:8b",
					Object:  "model",
					OwnedBy: "tokenio",
					Type:    "chat",
					Active:  true,
					Capabilities: domain.CapabilitySet{
						Chat: true,
					},
				},
			},
		},
	}
	router, err := NewRouter(
		authentication,
		models,
		&testRequestIDs{local: "llmreq_ollama_tags"},
	)
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, ollamaTagsPath, nil)
	request.Header.Set("Authorization", "Bearer sk_live_ollama")
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	if authentication.calls != 1 ||
		authentication.rawKey != "sk_live_ollama" ||
		models.calls != 1 ||
		models.family != domain.APIFamilyOllamaNative {
		t.Fatalf(
			"auth calls=%d raw=%q models calls=%d family=%q",
			authentication.calls,
			authentication.rawKey,
			models.calls,
			models.family,
		)
	}

	var catalog modelcatalogapp.Catalog
	if err := json.Unmarshal(response.Body.Bytes(), &catalog); err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	if catalog.Object != "list" || len(catalog.Data) != 1 || catalog.Data[0].ID != "llama3.1:8b" {
		t.Fatalf("catalog = %+v", catalog)
	}
}

func TestModelsEndpointRejectsQueryStringCredentials(t *testing.T) {
	for _, rawURL := range []string{
		modelsPath + "?key=sk_query",
		modelsPath + "?api_key=sk_query",
		modelsPath + "?access_token=sk_query",
		modelsPath + "?authorization=Bearer+sk_query",
	} {
		t.Run(rawURL, func(t *testing.T) {
			authentication := successfulAuthentication()
			models := &testModelCatalog{}
			router, err := NewRouter(
				authentication,
				models,
				&testRequestIDs{local: "llmreq_query_credential"},
			)
			if err != nil {
				t.Fatal(err)
			}

			request := httptest.NewRequest(http.MethodGet, rawURL, nil)
			request.Header.Set("Authorization", "Bearer sk_header")
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertError(
				t,
				response,
				http.StatusUnauthorized,
				domain.ErrorCodeUnauthorized,
				"query-string API keys are not allowed",
				"llmreq_query_credential",
			)
			if authentication.calls != 0 || models.calls != 0 {
				t.Fatalf(
					"auth calls=%d models calls=%d",
					authentication.calls,
					models.calls,
				)
			}
			if strings.Contains(response.Body.String(), "sk_") {
				t.Fatalf("error leaked credential: %s", response.Body.String())
			}
		})
	}
}

func TestModelsEndpointAuthorizationSyntax(
	t *testing.T,
) {
	tests := []struct {
		name          string
		authorization string
		message       string
	}{
		{
			name:    "missing",
			message: "Authorization header is required",
		},
		{
			name:          "wrong scheme",
			authorization: "Token sk_live_test",
			message:       "Authorization header format must be Bearer {api_key}",
		},
		{
			name:          "extra space",
			authorization: "Bearer  sk_live_test",
			message:       "Authorization header format must be Bearer {api_key}",
		},
		{
			name:          "wrong prefix",
			authorization: "Bearer token",
			message:       "API key must start with sk_",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authentication :=
				&testAuthentication{}
			models := &testModelCatalog{}
			router, err := NewRouter(
				authentication,
				models,
				&testRequestIDs{
					local: "llmreq_auth_1",
				},
			)
			if err != nil {
				t.Fatal(err)
			}

			request := httptest.NewRequest(
				http.MethodGet,
				modelsPath,
				nil,
			)
			if test.authorization != "" {
				request.Header.Set(
					"Authorization",
					test.authorization,
				)
			}
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertError(
				t,
				response,
				http.StatusUnauthorized,
				domain.ErrorCodeUnauthorized,
				test.message,
				"llmreq_auth_1",
			)
			if authentication.calls != 0 ||
				models.calls != 0 {
				t.Fatalf(
					"auth calls=%d models calls=%d",
					authentication.calls,
					models.calls,
				)
			}
		})
	}
}

func TestModelsEndpointMapsAuthenticationErrors(
	t *testing.T,
) {
	tests := []struct {
		name    string
		err     error
		status  int
		code    domain.ErrorCode
		message string
	}{
		{
			name:    "invalid key",
			err:     authenticateapp.ErrInvalidAPIKey,
			status:  http.StatusUnauthorized,
			code:    domain.ErrorCodeInvalidAPIKey,
			message: "Invalid API key",
		},
		{
			name:    "disabled user",
			err:     authenticateapp.ErrUserDisabled,
			status:  http.StatusForbidden,
			code:    domain.ErrorCodeUserDisabled,
			message: "User is disabled",
		},
		{
			name: "internal",
			err: errors.New(
				"database sk_live_secret",
			),
			status:  http.StatusInternalServerError,
			code:    domain.ErrorCodeInternalError,
			message: "Internal error",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authentication :=
				&testAuthentication{err: test.err}
			models := &testModelCatalog{}
			router, err := NewRouter(
				authentication,
				models,
				&testRequestIDs{
					local: "llmreq_auth_error",
				},
			)
			if err != nil {
				t.Fatal(err)
			}

			request := httptest.NewRequest(
				http.MethodGet,
				modelsPath,
				nil,
			)
			request.Header.Set(
				"Authorization",
				"Bearer sk_live_test",
			)
			response := httptest.NewRecorder()

			router.ServeHTTP(response, request)

			assertError(
				t,
				response,
				test.status,
				test.code,
				test.message,
				"llmreq_auth_error",
			)
			if models.calls != 0 {
				t.Fatalf(
					"model calls=%d",
					models.calls,
				)
			}
			if strings.Contains(
				response.Body.String(),
				"sk_live_",
			) {
				t.Fatalf(
					"error leaked raw key: %s",
					response.Body.String(),
				)
			}
		})
	}
}

func TestModelsEndpointMethodRestriction(
	t *testing.T,
) {
	models := &testModelCatalog{}
	router, err := NewRouter(
		successfulAuthentication(),
		models,
		&testRequestIDs{
			local: "llmreq_method",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodPost,
		modelsPath,
		nil,
	)
	request.Header.Set(
		"Authorization",
		"Bearer sk_live_test",
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assertError(
		t,
		response,
		http.StatusMethodNotAllowed,
		domain.ErrorCodeMethodNotAllowed,
		"Method is not allowed",
		"llmreq_method",
	)
	if response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf(
			"Allow = %q",
			response.Header().Get("Allow"),
		)
	}
	if models.calls != 0 {
		t.Fatalf("model calls=%d", models.calls)
	}
}

func TestModelsEndpointMapsCatalogFailureWithoutLeakage(
	t *testing.T,
) {
	models := &testModelCatalog{
		err: errors.Join(
			modelcatalogapp.ErrCatalogUnavailable,
			errors.New(
				"postgres password sk_live_secret",
			),
		),
	}
	router, err := NewRouter(
		successfulAuthentication(),
		models,
		&testRequestIDs{
			local: "llmreq_catalog_error",
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		modelsPath,
		nil,
	)
	request.Header.Set(
		"Authorization",
		"Bearer sk_live_test",
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assertError(
		t,
		response,
		http.StatusServiceUnavailable,
		domain.ErrorCodeStoreUnavailable,
		"Store is unavailable",
		"llmreq_catalog_error",
	)
	if strings.Contains(
		response.Body.String(),
		"postgres",
	) ||
		strings.Contains(
			response.Body.String(),
			"sk_live_",
		) {
		t.Fatalf(
			"catalog error leaked: %s",
			response.Body.String(),
		)
	}
}

func TestModelsEndpointRequestIDFailureStopsProcessing(
	t *testing.T,
) {
	authentication := &testAuthentication{}
	models := &testModelCatalog{}
	router, err := NewRouter(
		authentication,
		models,
		&testRequestIDs{
			err: errors.New("entropy unavailable"),
		},
	)
	if err != nil {
		t.Fatal(err)
	}

	request := httptest.NewRequest(
		http.MethodGet,
		modelsPath,
		nil,
	)
	request.Header.Set(
		"Authorization",
		"Bearer sk_live_test",
	)
	response := httptest.NewRecorder()

	router.ServeHTTP(response, request)

	assertError(
		t,
		response,
		http.StatusInternalServerError,
		domain.ErrorCodeInternalError,
		"Internal error",
		"",
	)
	if response.Header().Get(
		localRequestIDHeader,
	) != "" ||
		authentication.calls != 0 ||
		models.calls != 0 {
		t.Fatalf(
			"header=%q auth=%d models=%d",
			response.Header().Get(
				localRequestIDHeader,
			),
			authentication.calls,
			models.calls,
		)
	}
}

func assertError(
	t *testing.T,
	response *httptest.ResponseRecorder,
	status int,
	code domain.ErrorCode,
	message string,
	requestID string,
) {
	t.Helper()

	if response.Code != status {
		t.Fatalf(
			"status=%d want=%d body=%s",
			response.Code,
			status,
			response.Body.String(),
		)
	}
	if response.Header().Get(
		localRequestIDHeader,
	) != requestID {
		t.Fatalf(
			"request id header=%q want=%q",
			response.Header().Get(
				localRequestIDHeader,
			),
			requestID,
		)
	}

	var body errorResponse
	if err := json.Unmarshal(
		response.Body.Bytes(),
		&body,
	); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.Error.Code != code ||
		body.Error.Message != message ||
		body.Error.RequestID != requestID {
		t.Fatalf("error body=%+v", body)
	}
}
