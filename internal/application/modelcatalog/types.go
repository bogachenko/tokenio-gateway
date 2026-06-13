package modelcatalog

import "github.com/bogachenko/tokenio-gateway/internal/domain"

type Catalog struct {
	Object string  `json:"object"`
	Data   []Model `json:"data"`
}

type Model struct {
	ID           string               `json:"id"`
	Object       string               `json:"object"`
	OwnedBy      string               `json:"owned_by"`
	Type         string               `json:"type"`
	Active       bool                 `json:"active"`
	Pricing      *Pricing             `json:"pricing,omitempty"`
	Capabilities domain.CapabilitySet `json:"capabilities"`
}

type Pricing struct {
	Currency                             string                         `json:"currency"`
	InputPricePer1MTokensCents           int64                          `json:"input_price_per_1m_tokens_cents"`
	CachedInputPricePer1MTokensCents     int64                          `json:"cached_input_price_per_1m_tokens_cents"`
	OutputPricePer1MTokensCents          int64                          `json:"output_price_per_1m_tokens_cents"`
	ReasoningOutputPricePer1MTokensCents int64                          `json:"reasoning_output_price_per_1m_tokens_cents"`
	ImageInputPricePer1MTokensCents      int64                          `json:"image_input_price_per_1m_tokens_cents"`
	AudioInputPricePer1MTokensCents      int64                          `json:"audio_input_price_per_1m_tokens_cents"`
	AudioOutputPricePer1MTokensCents     int64                          `json:"audio_output_price_per_1m_tokens_cents"`
	FileInputPricePer1MTokensCents       int64                          `json:"file_input_price_per_1m_tokens_cents"`
	VideoInputPricePer1MTokensCents      int64                          `json:"video_input_price_per_1m_tokens_cents"`
	ImageGenerationPricePerUnitCents     int64                          `json:"image_generation_price_per_unit_cents"`
	ImageGenerationUnitKind              domain.ImageGenerationUnitKind `json:"image_generation_unit_kind"`
}
