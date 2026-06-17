package provisioninghttp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
	"time"

	application "github.com/bogachenko/tokenio-gateway/internal/application/provisioning"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/bogachenko/tokenio-gateway/internal/transport/httptransport"
)

const (
	basePath = "/internal/v1/api-key-provisionings"

	serviceTokenHeader = "X-Service-Token"
	idempotencyHeader  = "Idempotency-Key"
)

type Authenticator interface {
	Authenticate(string) error
}

type Service interface {
	Provision(
		context.Context,
		application.ProvisionInput,
	) (application.ProvisionResult, error)
	ConfirmDelivery(
		context.Context,
		string,
	) (application.ConfirmDeliveryResult, error)
}

type Router struct {
	service      Service
	auth         Authenticator
	ids          ports.RequestIDGenerator
	maxBodyBytes int64
}

func NewRouter(
	service Service,
	authenticator Authenticator,
	ids ports.RequestIDGenerator,
	maxBodyBytes int64,
) (*Router, error) {
	if service == nil ||
		authenticator == nil ||
		ids == nil ||
		maxBodyBytes <= 0 {
		return nil, errors.New(
			"provisioning router dependency is required",
		)
	}
	return &Router{
		service:      service,
		auth:         authenticator,
		ids:          ids,
		maxBodyBytes: maxBodyBytes,
	}, nil
}

func (h *Router) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	if request.URL.Path != basePath &&
		!strings.HasPrefix(
			request.URL.Path,
			basePath+"/",
		) {
		writeError(
			writer,
			"",
			http.StatusNotFound,
			domain.ErrorCodeNotFound,
			"Endpoint not found",
		)
		return
	}

	requestID, err :=
		h.ids.NewProvisioningRequestID()
	if err != nil ||
		!validProvisioningRequestID(requestID) {
		writeError(
			writer,
			"",
			http.StatusInternalServerError,
			domain.ErrorCodeInternalError,
			"Internal error",
		)
		return
	}

	if err := h.auth.Authenticate(
		request.Header.Get(serviceTokenHeader),
	); err != nil {
		writeError(
			writer,
			requestID,
			http.StatusUnauthorized,
			domain.ErrorCodeProvisioningUnauthorized,
			"Provisioning authorization failed",
		)
		return
	}

	if request.URL.Path == basePath {
		h.handleProvision(writer, request, requestID)
		return
	}
	h.handleProvisioningPath(
		writer,
		request,
		requestID,
	)
}

func (h *Router) handleProvision(
	writer http.ResponseWriter,
	request *http.Request,
	requestID string,
) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, requestID)
		return
	}
	if !isJSONContentType(
		request.Header.Get("Content-Type"),
	) {
		writeProvisioningInvalidRequest(
			writer,
			requestID,
		)
		return
	}

	idempotencyKey :=
		request.Header.Get(idempotencyHeader)
	if !validOpaque(idempotencyKey) {
		writeProvisioningInvalidRequest(
			writer,
			requestID,
		)
		return
	}

	var body provisionRequest
	if !decodeJSON(
		writer,
		request,
		h.maxBodyBytes,
		&body,
	) {
		writeProvisioningInvalidRequest(
			writer,
			requestID,
		)
		return
	}

	result, err := h.service.Provision(
		request.Context(),
		application.ProvisionInput{
			IdempotencyKey:        idempotencyKey,
			ExternalBillingUserID: body.ExternalBillingUserID,
			SourceReference:       body.SourceReference,
			KeyName:               body.KeyName,
		},
	)
	if err != nil {
		writeApplicationError(
			writer,
			requestID,
			err,
		)
		return
	}

	writeData(
		writer,
		http.StatusOK,
		provisionResponse{
			Result:             result.Result,
			ProvisioningID:     result.ProvisioningID,
			ProvisioningStatus: result.ProvisioningStatus,
			APIKeyID:           result.APIKeyID,
			APIKey:             result.APIKey,
			KeyPrefix:          result.KeyPrefix,
			ExpiresAt:          result.ExpiresAt,
		},
	)
}

func (h *Router) handleProvisioningPath(
	writer http.ResponseWriter,
	request *http.Request,
	requestID string,
) {
	path := strings.TrimPrefix(
		request.URL.Path,
		basePath+"/",
	)
	parts := strings.Split(path, "/")
	if len(parts) != 2 ||
		parts[0] == "" ||
		parts[1] != "confirm-delivery" {
		writeError(
			writer,
			requestID,
			http.StatusNotFound,
			domain.ErrorCodeNotFound,
			"Endpoint not found",
		)
		return
	}
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, requestID)
		return
	}

	result, err := h.service.ConfirmDelivery(
		request.Context(),
		parts[0],
	)
	if err != nil {
		writeApplicationError(
			writer,
			requestID,
			err,
		)
		return
	}

	writeData(
		writer,
		http.StatusOK,
		confirmDeliveryResponse{
			ProvisioningID: result.ProvisioningID,
			Status:         result.Status,
			DeliveredAt:    result.DeliveredAt,
		},
	)
}

type provisionRequest struct {
	ExternalBillingUserID string `json:"external_billing_user_id"`
	SourceReference       string `json:"source_reference"`
	KeyName               string `json:"key_name"`
}

type provisionResponse struct {
	Result application.ResultType `json:"result"`

	ProvisioningID     string                          `json:"provisioning_id"`
	ProvisioningStatus domain.APIKeyProvisioningStatus `json:"provisioning_status"`
	APIKeyID           string                          `json:"api_key_id"`

	APIKey    string     `json:"api_key,omitempty"`
	KeyPrefix string     `json:"key_prefix,omitempty"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type confirmDeliveryResponse struct {
	ProvisioningID string                          `json:"provisioning_id"`
	Status         domain.APIKeyProvisioningStatus `json:"status"`
	DeliveredAt    *time.Time                      `json:"delivered_at,omitempty"`
}

type dataEnvelope[T any] struct {
	Data T `json:"data"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      domain.ErrorCode `json:"code"`
	Message   string           `json:"message"`
	RequestID string           `json:"request_id,omitempty"`
}

func decodeJSON(
	writer http.ResponseWriter,
	request *http.Request,
	maxBodyBytes int64,
	destination any,
) bool {
	body := http.MaxBytesReader(
		writer,
		request.Body,
		maxBodyBytes,
	)
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(destination); err != nil {
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return false
	}
	return true
}

func isJSONContentType(value string) bool {
	if value == "" {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil &&
		mediaType == "application/json"
}

func validOpaque(value string) bool {
	return value != "" &&
		value == strings.TrimSpace(value)
}

func validProvisioningRequestID(value string) bool {
	return strings.HasPrefix(value, "provreq_") &&
		len(value) > len("provreq_")
}

func writeProvisioningInvalidRequest(
	writer http.ResponseWriter,
	requestID string,
) {
	writeError(
		writer,
		requestID,
		http.StatusBadRequest,
		domain.ErrorCodeProvisioningInvalidRequest,
		"Invalid provisioning request",
	)
}

func writeApplicationError(
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

func methodNotAllowed(
	writer http.ResponseWriter,
	requestID string,
) {
	writer.Header().Set("Allow", http.MethodPost)
	writeError(
		writer,
		requestID,
		http.StatusMethodNotAllowed,
		domain.ErrorCodeMethodNotAllowed,
		"Method is not allowed",
	)
}

func writeData[T any](
	writer http.ResponseWriter,
	status int,
	data T,
) {
	writer.Header().Set(
		"Content-Type",
		"application/json",
	)
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(
		dataEnvelope[T]{Data: data},
	)
}

func writeError(
	writer http.ResponseWriter,
	requestID string,
	status int,
	code domain.ErrorCode,
	message string,
) {
	writer.Header().Set(
		"Content-Type",
		"application/json",
	)
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(
		errorEnvelope{
			Error: errorBody{
				Code:      code,
				Message:   message,
				RequestID: requestID,
			},
		},
	)
}
