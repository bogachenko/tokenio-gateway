package publicapi

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	llmrequestapp "github.com/bogachenko/tokenio-gateway/internal/application/llmrequest"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
	"github.com/bogachenko/tokenio-gateway/internal/transport/httptransport"
)

const (
	chatCompletionsPath  = "/v1/chat/completions"
	embeddingsPath       = "/v1/embeddings"
	imageGenerationsPath = "/v1/images/generations"
	idempotencyKeyHeader = "Idempotency-Key"
)

type LLMRequest interface {
	Execute(
		context.Context,
		llmrequestapp.Input,
	) (llmrequestapp.ForwardedRequest, error)
}

type LLMRouter struct {
	requests     LLMRequest
	ids          ports.RequestIDGenerator
	bodyMaxBytes int64
}

func NewLLMRouter(
	requests LLMRequest,
	ids ports.RequestIDGenerator,
	bodyMaxBytes int64,
) (*LLMRouter, error) {
	if requests == nil || ids == nil || bodyMaxBytes <= 0 {
		return nil, errors.New("public LLM router dependency is required")
	}
	return &LLMRouter{
		requests:     requests,
		ids:          ids,
		bodyMaxBytes: bodyMaxBytes,
	}, nil
}

func (h *LLMRouter) ServeHTTP(
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
	writer.Header().Set(localRequestIDHeader, requestID)

	endpointKind, ok := llmEndpointKind(request.URL.Path)
	if !ok {
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
		writer.Header().Set("Allow", http.MethodPost)
		writeError(
			writer,
			requestID,
			http.StatusMethodNotAllowed,
			domain.ErrorCodeMethodNotAllowed,
			"Method is not allowed",
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

	body, err := httptransport.ReadJSONBodyLimited(
		request,
		h.bodyMaxBytes,
	)
	if err != nil {
		writeLLMBodyError(writer, requestID, err)
		return
	}

	var idempotencyKey *string
	if value := request.Header.Get(idempotencyKeyHeader); value != "" {
		copied := value
		idempotencyKey = &copied
	}

	result, err := h.requests.Execute(
		request.Context(),
		llmrequestapp.Input{
			LocalRequestID: requestID,
			RawAPIKey:      rawAPIKey,
			IdempotencyKey: idempotencyKey,
			APIFamily:      domain.APIFamilyOpenAICompatible,
			EndpointKind:   endpointKind,
			Payload:        body,
		},
	)
	if err != nil {
		writeError(
			writer,
			requestID,
			http.StatusInternalServerError,
			domain.ErrorCodeInternalError,
			"Internal error",
		)
		return
	}

	setKnownBillingHeaders(writer.Header(), result)
	upstreamHeaders := make(http.Header, len(result.Response.Headers))
	for key, values := range result.Response.Headers {
		upstreamHeaders[key] = append([]string(nil), values...)
	}
	if err := httptransport.WriteUpstreamSuccess(
		writer,
		result.Response.StatusCode,
		upstreamHeaders,
		result.Response.Body,
	); err != nil {
		writeError(
			writer,
			requestID,
			http.StatusInternalServerError,
			domain.ErrorCodeInternalError,
			"Internal error",
		)
	}
}

func llmEndpointKind(path string) (domain.EndpointKind, bool) {
	switch path {
	case chatCompletionsPath:
		return domain.EndpointChat, true
	case embeddingsPath:
		return domain.EndpointEmbeddings, true
	case imageGenerationsPath:
		return domain.EndpointImagesGeneration, true
	default:
		return "", false
	}
}

func writeLLMBodyError(
	writer http.ResponseWriter,
	requestID string,
	err error,
) {
	switch {
	case errors.Is(err, httptransport.ErrRequestBodyTooLarge):
		writeError(
			writer,
			requestID,
			http.StatusRequestEntityTooLarge,
			domain.ErrorCodeRequestBodyTooLarge,
			"Request body is too large",
		)
	case errors.Is(err, httptransport.ErrUnsupportedContentType):
		writeError(
			writer,
			requestID,
			http.StatusUnsupportedMediaType,
			domain.ErrorCodeUnsupportedContentType,
			"Content-Type must be application/json",
		)
	case errors.Is(err, httptransport.ErrInvalidJSON):
		writeError(
			writer,
			requestID,
			http.StatusBadRequest,
			domain.ErrorCodeInvalidJSON,
			"Request body must contain valid JSON",
		)
	default:
		writeError(
			writer,
			requestID,
			http.StatusBadRequest,
			domain.ErrorCodeInvalidJSON,
			"Request body could not be read",
		)
	}
}

func setKnownBillingHeaders(
	headers http.Header,
	result llmrequestapp.ForwardedRequest,
) {
	prepared := result.Reserved.Prepared
	admission := result.Reserved.Admission
	headers.Set(localRequestIDHeader, prepared.LocalRequestID)
	headers.Set(
		"X-Billing-Provider-Type",
		string(prepared.Plan.Route.ProviderType),
	)
	headers.Set("X-Billing-Client-Model", prepared.ClientModel)
	headers.Set("X-Billing-Model", prepared.Plan.BillingModel)
	headers.Set("X-Billing-Currency", prepared.Plan.Currency)
	headers.Set(
		"X-Wallet-Balance-Cents",
		strconv.FormatInt(admission.RemoteBalanceCents, 10),
	)
	headers.Set(
		"X-Wallet-Effective-Balance-Cents",
		strconv.FormatInt(admission.EffectiveBalanceCents, 10),
	)
	headers.Set(
		"X-Billing-Pending-Cents",
		strconv.FormatInt(admission.PendingAmountCents, 10),
	)
}
