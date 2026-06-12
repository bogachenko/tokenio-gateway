package routing

import "github.com/bogachenko/tokenio-gateway/internal/domain"

func Supports(available domain.CapabilitySet, requested domain.CapabilitySet) bool {
	return (!requested.Chat || available.Chat) &&
		(!requested.Embeddings || available.Embeddings) &&
		(!requested.ImagesGeneration || available.ImagesGeneration) &&
		(!requested.Tools || available.Tools) &&
		(!requested.ToolChoice || available.ToolChoice) &&
		(!requested.ResponseFormat || available.ResponseFormat) &&
		(!requested.JSONSchema || available.JSONSchema) &&
		(!requested.ImageInput || available.ImageInput) &&
		(!requested.AudioInput || available.AudioInput) &&
		(!requested.FileInput || available.FileInput) &&
		(!requested.VideoInput || available.VideoInput) &&
		(!requested.Reasoning || available.Reasoning)
}
