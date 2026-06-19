package nativeapi

import (
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type ModelSource string

const (
	ModelSourceNone ModelSource = "none"
	ModelSourceBody ModelSource = "body"
	ModelSourcePath ModelSource = "path"
)

type Operation string

const (
	OperationModels                Operation = "models"
	OperationChatCompletions       Operation = "chat_completions"
	OperationEmbeddings            Operation = "embeddings"
	OperationImagesGenerations     Operation = "images_generations"
	OperationAnthropicMessages     Operation = "anthropic_messages"
	OperationGeminiGenerateContent Operation = "gemini_generate_content"
	OperationGeminiStreamGenerate  Operation = "gemini_stream_generate_content"
	OperationGeminiEmbedContent    Operation = "gemini_embed_content"
	OperationGeminiBatchEmbeddings Operation = "gemini_batch_embed_contents"
	OperationOllamaChat            Operation = "ollama_chat"
	OperationOllamaGenerate        Operation = "ollama_generate"
	OperationOllamaEmbeddings      Operation = "ollama_embeddings"
	OperationOllamaTags            Operation = "ollama_tags"
)

type Contract struct {
	APIFamily    domain.APIFamily
	EndpointKind domain.EndpointKind
	Operation    Operation
	ModelSource  ModelSource
	PathModel    string
	Streaming    bool
}

func Resolve(method string, path string) (Contract, bool) {
	switch {
	case method == http.MethodGet && path == "/v1/models":
		return Contract{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointModels,
			Operation:    OperationModels,
			ModelSource:  ModelSourceNone,
		}, true
	case method == http.MethodPost && path == "/v1/chat/completions":
		return Contract{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointChat,
			Operation:    OperationChatCompletions,
			ModelSource:  ModelSourceBody,
		}, true
	case method == http.MethodPost && path == "/v1/embeddings":
		return Contract{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointEmbeddings,
			Operation:    OperationEmbeddings,
			ModelSource:  ModelSourceBody,
		}, true
	case method == http.MethodPost && path == "/v1/images/generations":
		return Contract{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: domain.EndpointImagesGeneration,
			Operation:    OperationImagesGenerations,
			ModelSource:  ModelSourceBody,
		}, true
	case method == http.MethodPost && path == "/v1/messages":
		return Contract{
			APIFamily:    domain.APIFamilyAnthropicNative,
			EndpointKind: domain.EndpointChat,
			Operation:    OperationAnthropicMessages,
			ModelSource:  ModelSourceBody,
		}, true
	case method == http.MethodGet && path == "/v1beta/models":
		return Contract{
			APIFamily:    domain.APIFamilyGeminiNative,
			EndpointKind: domain.EndpointModels,
			Operation:    OperationModels,
			ModelSource:  ModelSourceNone,
		}, true
	case strings.HasPrefix(path, "/v1beta/models/"):
		return resolveGeminiModelOperation(method, path)
	case method == http.MethodPost && path == "/api/chat":
		return Contract{
			APIFamily:    domain.APIFamilyOllamaNative,
			EndpointKind: domain.EndpointChat,
			Operation:    OperationOllamaChat,
			ModelSource:  ModelSourceBody,
		}, true
	case method == http.MethodPost && path == "/api/generate":
		return Contract{
			APIFamily:    domain.APIFamilyOllamaNative,
			EndpointKind: domain.EndpointChat,
			Operation:    OperationOllamaGenerate,
			ModelSource:  ModelSourceBody,
		}, true
	case method == http.MethodPost && path == "/api/embeddings":
		return Contract{
			APIFamily:    domain.APIFamilyOllamaNative,
			EndpointKind: domain.EndpointEmbeddings,
			Operation:    OperationOllamaEmbeddings,
			ModelSource:  ModelSourceBody,
		}, true
	case method == http.MethodGet && path == "/api/tags":
		return Contract{
			APIFamily:    domain.APIFamilyOllamaNative,
			EndpointKind: domain.EndpointModels,
			Operation:    OperationOllamaTags,
			ModelSource:  ModelSourceNone,
		}, true
	default:
		return Contract{}, false
	}
}

func resolveGeminiModelOperation(method string, path string) (Contract, bool) {
	tail := strings.TrimPrefix(path, "/v1beta/models/")
	model, operation, ok := strings.Cut(tail, ":")
	if !ok || model == "" || strings.Contains(model, "/") {
		return Contract{}, false
	}
	base := Contract{
		APIFamily:   domain.APIFamilyGeminiNative,
		ModelSource: ModelSourcePath,
		PathModel:   model,
	}
	switch {
	case method == http.MethodPost && operation == "generateContent":
		base.EndpointKind = domain.EndpointChat
		base.Operation = OperationGeminiGenerateContent
		return base, true
	case method == http.MethodPost && operation == "streamGenerateContent":
		base.EndpointKind = domain.EndpointChat
		base.Operation = OperationGeminiStreamGenerate
		base.Streaming = true
		return base, true
	case method == http.MethodPost && operation == "embedContent":
		base.EndpointKind = domain.EndpointEmbeddings
		base.Operation = OperationGeminiEmbedContent
		return base, true
	case method == http.MethodPost && operation == "batchEmbedContents":
		base.EndpointKind = domain.EndpointEmbeddings
		base.Operation = OperationGeminiBatchEmbeddings
		return base, true
	default:
		return Contract{}, false
	}
}

type Carrier string

const (
	CarrierAuthorizationBearer Carrier = "authorization_bearer"
	CarrierXAPIKey             Carrier = "x-api-key"
	CarrierXGoogAPIKey         Carrier = "x-goog-api-key"
)

type Credential struct {
	RawAPIKey string
	Carrier   Carrier
}

type CredentialFailure struct {
	Status int
	Reason string
}

func ExtractCredential(
	family domain.APIFamily,
	header http.Header,
	query url.Values,
) (Credential, *CredentialFailure) {
	if queryCredentialPresent(query) {
		return Credential{}, invalidCredential(
			"query-string API keys are not allowed",
		)
	}

	expected, ok := expectedCarrier(family)
	if !ok {
		return Credential{}, invalidCredential("unsupported API family")
	}

	var found []Credential
	for _, carrier := range []Carrier{
		CarrierAuthorizationBearer,
		CarrierXAPIKey,
		CarrierXGoogAPIKey,
	} {
		raw, present, failure := readCarrier(header, carrier)
		if failure != nil {
			return Credential{}, failure
		}
		if !present {
			continue
		}
		if !validRawAPIKey(raw) {
			return Credential{}, invalidCredential(
				"API key must start with sk_",
			)
		}
		found = append(found, Credential{RawAPIKey: raw, Carrier: carrier})
	}

	if len(found) == 0 {
		return Credential{}, invalidCredential("API key is required")
	}
	if len(found) != 1 || found[0].Carrier != expected {
		return Credential{}, invalidCredential(
			"credential carrier conflicts with API family",
		)
	}
	return found[0], nil
}

func expectedCarrier(family domain.APIFamily) (Carrier, bool) {
	switch family {
	case domain.APIFamilyOpenAICompatible, domain.APIFamilyOllamaNative:
		return CarrierAuthorizationBearer, true
	case domain.APIFamilyAnthropicNative:
		return CarrierXAPIKey, true
	case domain.APIFamilyGeminiNative:
		return CarrierXGoogAPIKey, true
	default:
		return "", false
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

func readCarrier(
	header http.Header,
	carrier Carrier,
) (string, bool, *CredentialFailure) {
	switch carrier {
	case CarrierAuthorizationBearer:
		values := header.Values("Authorization")
		if len(values) == 0 {
			return "", false, nil
		}
		if len(values) != 1 {
			return "", false, invalidCredential(
				"multiple Authorization headers are not allowed",
			)
		}
		raw, ok := parseBearer(values[0])
		if !ok {
			return "", false, invalidCredential(
				"Authorization header format must be Bearer {api_key}",
			)
		}
		return raw, true, nil
	case CarrierXAPIKey:
		return readSingleHeader(header, "x-api-key")
	case CarrierXGoogAPIKey:
		return readSingleHeader(header, "x-goog-api-key")
	default:
		return "", false, invalidCredential("unsupported credential carrier")
	}
}

func readSingleHeader(
	header http.Header,
	name string,
) (string, bool, *CredentialFailure) {
	values := headerValues(header, name)
	if len(values) == 0 {
		return "", false, nil
	}
	if len(values) != 1 {
		return "", false, invalidCredential(
			"multiple API key headers are not allowed",
		)
	}
	value := values[0]
	if value != strings.TrimSpace(value) || containsControlOrSpace(value) {
		return "", false, invalidCredential(
			name + " header contains invalid API key",
		)
	}
	return value, true, nil
}

func headerValues(header http.Header, name string) []string {
	values := append([]string(nil), header.Values(name)...)
	canonical := http.CanonicalHeaderKey(name)
	for key, item := range header {
		if strings.EqualFold(key, name) && key != canonical {
			values = append(values, item...)
		}
	}
	return values
}

func parseBearer(value string) (string, bool) {
	if value != strings.TrimSpace(value) {
		return "", false
	}
	parts := strings.Split(value, " ")
	if len(parts) != 2 || parts[0] != "Bearer" || parts[1] == "" {
		return "", false
	}
	if containsControlOrSpace(parts[1]) {
		return "", false
	}
	return parts[1], true
}

func validRawAPIKey(value string) bool {
	return strings.HasPrefix(value, "sk_") && !containsControlOrSpace(value)
}

func containsControlOrSpace(value string) bool {
	for len(value) > 0 {
		r, size := utf8.DecodeRuneInString(value)
		if r == utf8.RuneError && size == 1 {
			return true
		}
		if r <= 0x20 || r == 0x7f {
			return true
		}
		value = value[size:]
	}
	return false
}

func invalidCredential(reason string) *CredentialFailure {
	return &CredentialFailure{
		Status: http.StatusUnauthorized,
		Reason: reason,
	}
}
