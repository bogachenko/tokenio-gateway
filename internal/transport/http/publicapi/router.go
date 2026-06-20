package publicapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	modelcatalogapp "github.com/bogachenko/tokenio-gateway/internal/application/modelcatalog"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/bogachenko/tokenio-gateway/internal/transport/httptransport"
)

const (
	modelsPath           = "/v1/models"
	geminiModelsPath     = "/v1beta/models"
	ollamaTagsPath       = "/api/tags"
	localRequestIDHeader = "X-Local-Request-ID"
	localRequestIDPrefix = "llmreq_"
)

type Authentication interface {
	AuthenticatePublicRequest(
		context.Context,
		authenticateapp.Input,
	) (authenticateapp.Result, error)
}

type ModelCatalog interface {
	List(
		context.Context,
		domain.APIFamily,
	) (modelcatalogapp.Catalog, error)
}

type Router struct {
	authentication Authentication
	models         ModelCatalog
	ids            ports.RequestIDGenerator
}

func NewRouter(
	authentication Authentication,
	models ModelCatalog,
	ids ports.RequestIDGenerator,
) (*Router, error) {
	if authentication == nil ||
		models == nil ||
		ids == nil {
		return nil, errors.New(
			"public API router dependency is required",
		)
	}
	return &Router{
		authentication: authentication,
		models:         models,
		ids:            ids,
	}, nil
}

func (h *Router) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	requestID, err := h.ids.NewLocalRequestID()
	if err != nil || !validLocalRequestID(requestID) {
		writeError(
			writer,
			"",
			http.StatusInternalServerError,
			domain.ErrorCodeInternalError,
			"Internal error",
		)
		return
	}
	writer.Header().Set(
		localRequestIDHeader,
		requestID,
	)

	if request.URL.Path != modelsPath &&
		request.URL.Path != geminiModelsPath &&
		request.URL.Path != ollamaTagsPath {
		writeError(
			writer,
			requestID,
			http.StatusNotFound,
			domain.ErrorCodeNotFound,
			"Endpoint not found",
		)
		return
	}

	if queryCredentialPresent(request.URL.Query()) {
		writeError(
			writer,
			requestID,
			http.StatusUnauthorized,
			domain.ErrorCodeUnauthorized,
			"query-string API keys are not allowed",
		)
		return
	}

	apiFamily := modelCatalogFamily(request.URL.Path)
	rawAPIKey, authFailure := parseCatalogCredential(apiFamily, request.Header)
	if authFailure != nil {
		writeError(
			writer,
			requestID,
			authFailure.status,
			authFailure.code,
			authFailure.message,
		)
		return
	}

	_, err = h.authentication.AuthenticatePublicRequest(
		request.Context(),
		authenticateapp.Input{
			RawAPIKey: rawAPIKey,
		},
	)
	if err != nil {
		writeAuthenticationError(
			writer,
			requestID,
			err,
		)
		return
	}

	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeError(
			writer,
			requestID,
			http.StatusMethodNotAllowed,
			domain.ErrorCodeMethodNotAllowed,
			"Method is not allowed",
		)
		return
	}

	catalog, err := h.models.List(
		request.Context(),
		apiFamily,
	)
	if err != nil {
		writeCatalogError(
			writer,
			requestID,
			err,
		)
		return
	}

	writeJSON(
		writer,
		http.StatusOK,
		catalog,
	)
}

func modelCatalogFamily(path string) domain.APIFamily {
	switch path {
	case geminiModelsPath:
		return domain.APIFamilyGeminiNative
	case ollamaTagsPath:
		return domain.APIFamilyOllamaNative
	default:
		return domain.APIFamilyOpenAICompatible
	}
}

func queryCredentialPresent(query url.Values) bool {
	for key, values := range query {
		if len(values) == 0 {
			continue
		}
		switch strings.ToLower(key) {
		case "key",
			"api_key",
			"api-key",
			"access_token",
			"token",
			"authorization",
			"x-api-key",
			"x-goog-api-key",
			"openai_api_key",
			"anthropic_api_key",
			"google_api_key":
			return true
		}
	}
	return false
}

type authorizationFailure struct {
	status  int
	code    domain.ErrorCode
	message string
}

func parseCatalogCredential(apiFamily domain.APIFamily, headers http.Header) (string, *authorizationFailure) {
	if apiFamily == domain.APIFamilyGeminiNative {
		return parseGeminiAPIKey(headers)
	}
	return parseAuthorization(headers.Get("Authorization"))
}

func parseGeminiAPIKey(headers http.Header) (string, *authorizationFailure) {
	if headers.Get("Authorization") != "" {
		return "", invalidAuthorizationFormat()
	}
	value := headers.Get("x-goog-api-key")
	if value == "" {
		return "", &authorizationFailure{
			status:  http.StatusUnauthorized,
			code:    domain.ErrorCodeUnauthorized,
			message: "x-goog-api-key header is required",
		}
	}
	if value != strings.TrimSpace(value) || containsControlOrSpace(value) {
		return "", invalidAuthorizationFormat()
	}
	if !strings.HasPrefix(value, "sk_") {
		return "", &authorizationFailure{
			status:  http.StatusUnauthorized,
			code:    domain.ErrorCodeUnauthorized,
			message: "API key must start with sk_",
		}
	}
	return value, nil
}

func parseAuthorization(
	value string,
) (string, *authorizationFailure) {
	if value == "" {
		return "", &authorizationFailure{
			status:  http.StatusUnauthorized,
			code:    domain.ErrorCodeUnauthorized,
			message: "Authorization header is required",
		}
	}
	if value != strings.TrimSpace(value) {
		return "", invalidAuthorizationFormat()
	}

	parts := strings.Split(value, " ")
	if len(parts) != 2 ||
		parts[0] != "Bearer" ||
		parts[1] == "" ||
		containsControlOrSpace(parts[1]) {
		return "", invalidAuthorizationFormat()
	}
	if !strings.HasPrefix(parts[1], "sk_") {
		return "", &authorizationFailure{
			status:  http.StatusUnauthorized,
			code:    domain.ErrorCodeUnauthorized,
			message: "API key must start with sk_",
		}
	}
	return parts[1], nil
}

func invalidAuthorizationFormat() *authorizationFailure {
	return &authorizationFailure{
		status:  http.StatusUnauthorized,
		code:    domain.ErrorCodeUnauthorized,
		message: "Authorization header format must be Bearer {api_key}",
	}
}

func writeAuthenticationError(
	writer http.ResponseWriter,
	requestID string,
	err error,
) {
	applicationError, ok := ports.AsApplicationError(err)
	if !ok {
		writeError(
			writer,
			requestID,
			http.StatusInternalServerError,
			domain.ErrorCodeInternalError,
			"Internal error",
		)
		return
	}
	writeError(
		writer,
		requestID,
		httptransport.StatusForApplicationError(applicationError),
		applicationError.Code,
		applicationError.SafeMessage,
	)
}

func writeCatalogError(
	writer http.ResponseWriter,
	requestID string,
	err error,
) {
	applicationError, ok := ports.AsApplicationError(err)
	if !ok {
		writeError(
			writer,
			requestID,
			http.StatusInternalServerError,
			domain.ErrorCodeInternalError,
			"Internal error",
		)
		return
	}

	writeError(
		writer,
		requestID,
		httptransport.StatusForApplicationError(applicationError),
		applicationError.Code,
		applicationError.SafeMessage,
	)
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      domain.ErrorCode `json:"code"`
	Message   string           `json:"message"`
	RequestID string           `json:"request_id,omitempty"`
}

func writeError(
	writer http.ResponseWriter,
	requestID string,
	status int,
	code domain.ErrorCode,
	message string,
) {
	writeJSON(
		writer,
		status,
		errorResponse{
			Error: errorBody{
				Code:      code,
				Message:   message,
				RequestID: requestID,
			},
		},
	)
}

func writeJSON(
	writer http.ResponseWriter,
	status int,
	value any,
) {
	writer.Header().Set(
		"Content-Type",
		"application/json",
	)
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}

func validLocalRequestID(value string) bool {
	if !strings.HasPrefix(
		value,
		localRequestIDPrefix,
	) ||
		len(value) == len(localRequestIDPrefix) {
		return false
	}
	return !containsControlOrSpace(value)
}

func containsControlOrSpace(value string) bool {
	for len(value) > 0 {
		current, size := utf8.DecodeRuneInString(value)
		if current == utf8.RuneError &&
			size == 1 {
			return true
		}
		if current <= 0x20 || current == 0x7f {
			return true
		}
		value = value[size:]
	}
	return false
}
