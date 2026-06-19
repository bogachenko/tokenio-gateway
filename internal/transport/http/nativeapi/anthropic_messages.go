package nativeapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	authenticateapp "github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/bogachenko/tokenio-gateway/internal/transport/httptransport"
)

const (
	anthropicMessagesPath = "/v1/messages"
	maxNativeBodyBytes    = 1 << 20
)

type Authentication interface {
	AuthenticatePublicRequest(
		context.Context,
		authenticateapp.Input,
	) (authenticateapp.Result, error)
}

type AnthropicMessages interface {
	HandleAnthropicMessages(
		context.Context,
		AnthropicMessagesInput,
	) (AnthropicMessagesResult, error)
}

type AnthropicMessagesInput struct {
	Authentication authenticateapp.Result
	Model          string
	RawBody        []byte
}

type AnthropicMessagesResult struct {
	Status  int
	Header  http.Header
	RawBody []byte
}

type AnthropicMessagesRouter struct {
	authentication Authentication
	messages       AnthropicMessages
	ids            ports.RequestIDGenerator
}

func NewAnthropicMessagesRouter(
	authentication Authentication,
	messages AnthropicMessages,
	ids ports.RequestIDGenerator,
) (*AnthropicMessagesRouter, error) {
	if authentication == nil || messages == nil || ids == nil {
		return nil, errors.New(
			"anthropic messages router dependency is required",
		)
	}
	return &AnthropicMessagesRouter{
		authentication: authentication,
		messages:       messages,
		ids:            ids,
	}, nil
}

func (r *AnthropicMessagesRouter) ServeHTTP(
	writer http.ResponseWriter,
	request *http.Request,
) {
	requestID, err := r.ids.NewLocalRequestID()
	if err != nil || !validNativeLocalRequestID(requestID) {
		writeNativeError(
			writer,
			"",
			http.StatusInternalServerError,
			domain.ErrorCodeInternalError,
			"Internal error",
		)
		return
	}
	writer.Header().Set("X-Local-Request-ID", requestID)

	contract, ok := Resolve(request.Method, request.URL.Path)
	if !ok || contract.APIFamily != domain.APIFamilyAnthropicNative ||
		contract.Operation != OperationAnthropicMessages {
		writeNativeError(
			writer,
			requestID,
			http.StatusNotFound,
			domain.ErrorCodeNotFound,
			"Endpoint not found",
		)
		return
	}

	credential, failure := ExtractCredential(
		contract.APIFamily,
		request.Header,
		request.URL.Query(),
	)
	if failure != nil {
		writeNativeError(
			writer,
			requestID,
			failure.Status,
			domain.ErrorCodeUnauthorized,
			failure.Reason,
		)
		return
	}

	authentication, err := r.authentication.AuthenticatePublicRequest(
		request.Context(),
		authenticateapp.Input{RawAPIKey: credential.RawAPIKey},
	)
	if err != nil {
		writeNativeAuthenticationError(writer, requestID, err)
		return
	}

	body, model, err := readAnthropicMessagesBody(request)
	if err != nil {
		writeNativeError(
			writer,
			requestID,
			http.StatusBadRequest,
			domain.ErrorCodeModelRequired,
			err.Error(),
		)
		return
	}

	result, err := r.messages.HandleAnthropicMessages(
		request.Context(),
		AnthropicMessagesInput{
			Authentication: authentication,
			Model:          model,
			RawBody:        body,
		},
	)
	if err != nil {
		writeNativeApplicationError(writer, requestID, err)
		return
	}

	writeNativeRaw(writer, result)
}

func readAnthropicMessagesBody(
	request *http.Request,
) ([]byte, string, error) {
	if request.Body == nil {
		return nil, "", errors.New("request body is required")
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxNativeBodyBytes+1))
	if err != nil {
		return nil, "", errors.New("request body is unreadable")
	}
	if len(body) == 0 {
		return nil, "", errors.New("request body is required")
	}
	if len(body) > maxNativeBodyBytes {
		return nil, "", errors.New("request body is too large")
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var payload struct {
		Model string `json:"model"`
	}
	if err := decoder.Decode(&payload); err != nil {
		return nil, "", errors.New("request body must be valid JSON")
	}
	var trailing struct{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, "", errors.New("request body must contain one JSON value")
	}
	model := strings.TrimSpace(payload.Model)
	if model == "" || containsControlOrSpace(model) {
		return nil, "", errors.New("model is required")
	}
	return body, model, nil
}

func writeNativeRaw(
	writer http.ResponseWriter,
	result AnthropicMessagesResult,
) {
	status := result.Status
	if status == 0 {
		status = http.StatusOK
	}
	for key, values := range result.Header {
		if strings.EqualFold(key, "Authorization") ||
			strings.EqualFold(key, "x-api-key") ||
			strings.EqualFold(key, "x-goog-api-key") {
			continue
		}
		for _, value := range values {
			writer.Header().Add(key, value)
		}
	}
	if writer.Header().Get("Content-Type") == "" {
		writer.Header().Set("Content-Type", "application/json")
	}
	writer.WriteHeader(status)
	_, _ = writer.Write(result.RawBody)
}

func writeNativeAuthenticationError(
	writer http.ResponseWriter,
	requestID string,
	err error,
) {
	writeNativeApplicationError(writer, requestID, err)
}

func writeNativeApplicationError(
	writer http.ResponseWriter,
	requestID string,
	err error,
) {
	applicationError, ok := ports.AsApplicationError(err)
	if !ok {
		writeNativeError(
			writer,
			requestID,
			http.StatusInternalServerError,
			domain.ErrorCodeInternalError,
			"Internal error",
		)
		return
	}

	writeNativeError(
		writer,
		requestID,
		httptransport.StatusForApplicationError(applicationError),
		applicationError.Code,
		applicationError.SafeMessage,
	)
}

type nativeErrorResponse struct {
	Error nativeErrorBody `json:"error"`
}

type nativeErrorBody struct {
	Code      domain.ErrorCode `json:"code"`
	Message   string           `json:"message"`
	RequestID string           `json:"request_id,omitempty"`
}

func writeNativeError(
	writer http.ResponseWriter,
	requestID string,
	status int,
	code domain.ErrorCode,
	message string,
) {
	writer.Header().Set("Content-Type", "application/json")
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(nativeErrorResponse{
		Error: nativeErrorBody{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	})
}

func validNativeLocalRequestID(value string) bool {
	return strings.HasPrefix(value, "llmreq_") && !containsControlOrSpace(value)
}
