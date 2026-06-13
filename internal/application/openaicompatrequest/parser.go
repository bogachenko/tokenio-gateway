package openaicompatrequest

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

const maxJSONDepth = 128

var errInvalidJSONStructure = errors.New(
	"invalid JSON structure",
)

func Parse(
	endpoint domain.EndpointKind,
	body []byte,
) (ParsedRequest, error) {
	if !supportedEndpoint(endpoint) {
		return ParsedRequest{}, ErrUnsupportedEndpoint
	}
	if err := validateJSONStructure(body); err != nil {
		return ParsedRequest{}, ErrInvalidJSON
	}

	var root map[string]json.RawMessage
	if err := json.Unmarshal(body, &root); err != nil ||
		root == nil {
		return ParsedRequest{}, ErrInvalidJSON
	}

	modelRaw, exists := root["model"]
	if !exists {
		return ParsedRequest{}, ErrModelRequired
	}
	var model string
	if err := json.Unmarshal(
		modelRaw,
		&model,
	); err != nil {
		return ParsedRequest{}, ErrInvalidJSON
	}
	if strings.TrimSpace(model) == "" {
		return ParsedRequest{}, ErrModelRequired
	}

	if streamRaw, exists := root["stream"]; exists {
		var stream bool
		if err := json.Unmarshal(
			streamRaw,
			&stream,
		); err != nil {
			return ParsedRequest{}, ErrInvalidJSON
		}
		if stream {
			return ParsedRequest{},
				ErrStreamingUnsupported
		}
	}

	capabilities := endpointCapabilities(endpoint)
	if endpoint == domain.EndpointChat {
		capabilities = chatCapabilities(
			root,
			capabilities,
		)
	}

	return ParsedRequest{
		Body: bytes.Clone(body),
		Query: ports.RouteQuery{
			APIFamily:    domain.APIFamilyOpenAICompatible,
			EndpointKind: endpoint,
			ClientModel:  model,
		},
		RequestedCapabilities: capabilities,
	}, nil
}

func supportedEndpoint(
	endpoint domain.EndpointKind,
) bool {
	switch endpoint {
	case domain.EndpointChat,
		domain.EndpointEmbeddings,
		domain.EndpointImagesGeneration:
		return true
	default:
		return false
	}
}

func endpointCapabilities(
	endpoint domain.EndpointKind,
) domain.CapabilitySet {
	switch endpoint {
	case domain.EndpointChat:
		return domain.CapabilitySet{Chat: true}
	case domain.EndpointEmbeddings:
		return domain.CapabilitySet{
			Embeddings: true,
		}
	case domain.EndpointImagesGeneration:
		return domain.CapabilitySet{
			ImagesGeneration: true,
		}
	default:
		return domain.CapabilitySet{}
	}
}

func chatCapabilities(
	root map[string]json.RawMessage,
	capabilities domain.CapabilitySet,
) domain.CapabilitySet {
	if _, exists := root["tools"]; exists {
		capabilities.Tools = true
	}
	if _, exists := root["tool_choice"]; exists {
		capabilities.ToolChoice = true
	}
	if responseFormat, exists :=
		root["response_format"]; exists {
		capabilities.ResponseFormat = true
		if requestsJSONSchema(responseFormat) {
			capabilities.JSONSchema = true
		}
	}
	if _, exists := root["reasoning_effort"]; exists {
		capabilities.Reasoning = true
	}

	messagesRaw, exists := root["messages"]
	if !exists {
		return capabilities
	}

	var messages []json.RawMessage
	if err := json.Unmarshal(
		messagesRaw,
		&messages,
	); err != nil {
		return capabilities
	}
	for _, messageRaw := range messages {
		var message map[string]json.RawMessage
		if err := json.Unmarshal(
			messageRaw,
			&message,
		); err != nil ||
			message == nil {
			continue
		}
		contentRaw, exists := message["content"]
		if !exists {
			continue
		}
		capabilities = contentCapabilities(
			contentRaw,
			capabilities,
		)
	}
	return capabilities
}

func requestsJSONSchema(
	responseFormat json.RawMessage,
) bool {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(
		responseFormat,
		&value,
	); err != nil ||
		value == nil {
		return false
	}

	if _, exists := value["json_schema"]; exists {
		return true
	}

	typeRaw, exists := value["type"]
	if !exists {
		return false
	}
	var formatType string
	if err := json.Unmarshal(
		typeRaw,
		&formatType,
	); err != nil {
		return false
	}
	return formatType == "json_schema"
}

func contentCapabilities(
	content json.RawMessage,
	capabilities domain.CapabilitySet,
) domain.CapabilitySet {
	var parts []json.RawMessage
	if err := json.Unmarshal(
		content,
		&parts,
	); err == nil {
		for _, part := range parts {
			capabilities = contentPartCapabilities(
				part,
				capabilities,
			)
		}
		return capabilities
	}

	return contentPartCapabilities(
		content,
		capabilities,
	)
}

func contentPartCapabilities(
	part json.RawMessage,
	capabilities domain.CapabilitySet,
) domain.CapabilitySet {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(
		part,
		&value,
	); err != nil ||
		value == nil {
		return capabilities
	}

	partType := ""
	if typeRaw, exists := value["type"]; exists {
		_ = json.Unmarshal(typeRaw, &partType)
	}

	switch partType {
	case "image_url", "input_image":
		capabilities.ImageInput = true
	case "input_audio", "audio":
		capabilities.AudioInput = true
	case "file", "input_file", "document":
		capabilities.FileInput = true
	case "video_url", "input_video":
		capabilities.VideoInput = true
	}

	if hasAnyKey(
		value,
		"image_url",
		"input_image",
	) {
		capabilities.ImageInput = true
	}
	if hasAnyKey(
		value,
		"input_audio",
		"audio",
	) {
		capabilities.AudioInput = true
	}
	if hasAnyKey(
		value,
		"file",
		"file_id",
		"input_file",
		"document",
	) {
		capabilities.FileInput = true
	}
	if hasAnyKey(
		value,
		"video_url",
		"input_video",
	) {
		capabilities.VideoInput = true
	}

	return capabilities
}

func hasAnyKey(
	value map[string]json.RawMessage,
	keys ...string,
) bool {
	for _, key := range keys {
		if _, exists := value[key]; exists {
			return true
		}
	}
	return false
}

func validateJSONStructure(body []byte) error {
	if len(body) == 0 || !utf8.Valid(body) {
		return errInvalidJSONStructure
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()

	if err := scanJSONValue(
		decoder,
		0,
	); err != nil {
		return errInvalidJSONStructure
	}

	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errInvalidJSONStructure
	}
	return nil
}

func scanJSONValue(
	decoder *json.Decoder,
	depth int,
) error {
	if depth > maxJSONDepth {
		return errInvalidJSONStructure
	}

	token, err := decoder.Token()
	if err != nil {
		return errInvalidJSONStructure
	}

	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return errInvalidJSONStructure
			}
			key, ok := keyToken.(string)
			if !ok {
				return errInvalidJSONStructure
			}
			if _, exists := seen[key]; exists {
				return errInvalidJSONStructure
			}
			seen[key] = struct{}{}
			if err := scanJSONValue(
				decoder,
				depth+1,
			); err != nil {
				return err
			}
		}
		closeToken, err := decoder.Token()
		if err != nil ||
			closeToken != json.Delim('}') {
			return errInvalidJSONStructure
		}
		return nil

	case '[':
		for decoder.More() {
			if err := scanJSONValue(
				decoder,
				depth+1,
			); err != nil {
				return err
			}
		}
		closeToken, err := decoder.Token()
		if err != nil ||
			closeToken != json.Delim(']') {
			return errInvalidJSONStructure
		}
		return nil

	default:
		return errInvalidJSONStructure
	}
}
