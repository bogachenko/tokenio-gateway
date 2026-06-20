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
	"github.com/bogachenko/tokenio-gateway/internal/transport/http/nativeapi"
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

	apiFamily := modelCatalogFamily(request.URL.Path)
	credential, credentialFailure := nativeapi.ExtractCredential(
		apiFamily,
		request.Header,
		request.URL.Query(),
	)
	if credentialFailure != nil {
		writeError(
			writer,
			requestID,
			credentialFailure.Status,
			domain.ErrorCodeUnauthorized,
			credentialFailure.Reason,
		)
		return
	}
	if !strings.HasPrefix(credential.RawAPIKey, "sk_") {
		writeError(
			writer,
			requestID,
			http.StatusUnauthorized,
			domain.ErrorCodeUnauthorized,
			"API key must start with sk_",
		)
		return
	}

	_, err = h.authentication.AuthenticatePublicRequest(
		request.Context(),
		authenticateapp.Input{
			RawAPIKey: credential.RawAPIKey,
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
