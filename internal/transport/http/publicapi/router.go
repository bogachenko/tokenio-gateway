package publicapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	modelcatalogapp "github.com/bogachenko/tokenio-gateway/internal/application/modelcatalog"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const (
	modelsPath           = "/v1/models"
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

	if request.URL.Path != modelsPath {
		writeError(
			writer,
			requestID,
			http.StatusNotFound,
			domain.ErrorCodeNotFound,
			"Endpoint not found",
		)
		return
	}

	rawAPIKey, authFailure := parseAuthorization(
		request.Header.Get("Authorization"),
	)
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
		domain.APIFamilyOpenAICompatible,
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

type authorizationFailure struct {
	status  int
	code    domain.ErrorCode
	message string
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
		statusForApplicationError(applicationError),
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
		statusForApplicationError(applicationError),
		applicationError.Code,
		applicationError.SafeMessage,
	)
}

func statusForApplicationError(
	applicationError *ports.ApplicationError,
) int {
	if applicationError == nil {
		return http.StatusInternalServerError
	}
	switch applicationError.Category {
	case ports.FailureCategoryInvalidRequest:
		return http.StatusBadRequest
	case ports.FailureCategoryUnauthorized:
		return http.StatusUnauthorized
	case ports.FailureCategoryForbidden:
		return http.StatusForbidden
	case ports.FailureCategoryConflict:
		return http.StatusConflict
	case ports.FailureCategoryDependencyUnavailable:
		return http.StatusBadGateway
	case ports.FailureCategoryUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
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
