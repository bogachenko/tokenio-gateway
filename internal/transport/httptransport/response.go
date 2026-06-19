package httptransport

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

var ErrNonSuccessfulUpstreamStatus = errors.New("non-successful upstream status cannot be passed through as success")

type BillingHeaders struct {
	LocalRequestID              string
	ProviderType                domain.ProviderType
	ClientModel                 string
	BillingModel                string
	InputTokens                 int64
	CachedInputTokens           int64
	OutputTokens                int64
	ReasoningTokens             int64
	ImageInputTokens            int64
	AudioInputTokens            int64
	AudioOutputTokens           int64
	FileInputTokens             int64
	VideoInputTokens            int64
	ImageGenerationUnits        int64
	ClientAmountCents           int64
	Currency                    string
	WalletBalanceCents          int64
	WalletEffectiveBalanceCents int64
	BillingPendingCents         int64
}

func WriteUpstreamSuccess(writer http.ResponseWriter, status int, headers http.Header, body []byte) error {
	if status < http.StatusOK || status > 299 {
		return ErrNonSuccessfulUpstreamStatus
	}
	copySafeResponseHeaders(writer.Header(), headers)
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
	return nil
}

func copySafeResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		if !isSafeUpstreamResponseHeader(key) {
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func isSafeUpstreamResponseHeader(key string) bool {
	lower := strings.ToLower(key)
	switch lower {
	case "content-length",
		"connection",
		"keep-alive",
		"proxy-authenticate",
		"proxy-authorization",
		"te",
		"trailer",
		"transfer-encoding",
		"upgrade":
		return false
	}
	return lower != "x-local-request-id" &&
		!strings.HasPrefix(lower, "x-billing-") &&
		!strings.HasPrefix(lower, "x-wallet-")
}

func SetBillingHeaders(h http.Header, headers BillingHeaders) {
	setOptionalHeader(h, "X-Local-Request-ID", headers.LocalRequestID)
	setOptionalHeader(h, "X-Billing-Provider-Type", string(headers.ProviderType))
	setOptionalHeader(h, "X-Billing-Client-Model", headers.ClientModel)
	setOptionalHeader(h, "X-Billing-Model", headers.BillingModel)
	setIntHeader(h, "X-Billing-Input-Tokens", headers.InputTokens)
	setIntHeader(h, "X-Billing-Cached-Input-Tokens", headers.CachedInputTokens)
	setIntHeader(h, "X-Billing-Output-Tokens", headers.OutputTokens)
	setIntHeader(h, "X-Billing-Reasoning-Tokens", headers.ReasoningTokens)
	setIntHeader(h, "X-Billing-Image-Input-Tokens", headers.ImageInputTokens)
	setIntHeader(h, "X-Billing-Audio-Input-Tokens", headers.AudioInputTokens)
	setIntHeader(h, "X-Billing-Audio-Output-Tokens", headers.AudioOutputTokens)
	setIntHeader(h, "X-Billing-File-Input-Tokens", headers.FileInputTokens)
	setIntHeader(h, "X-Billing-Video-Input-Tokens", headers.VideoInputTokens)
	setIntHeader(h, "X-Billing-Image-Generation-Units", headers.ImageGenerationUnits)
	setIntHeader(h, "X-Billing-Amount-Cents", headers.ClientAmountCents)
	setOptionalHeader(h, "X-Billing-Currency", headers.Currency)
	setIntHeader(h, "X-Wallet-Balance-Cents", headers.WalletBalanceCents)
	setIntHeader(h, "X-Wallet-Effective-Balance-Cents", headers.WalletEffectiveBalanceCents)
	setIntHeader(h, "X-Billing-Pending-Cents", headers.BillingPendingCents)
}

func setOptionalHeader(h http.Header, key string, value string) {
	if strings.TrimSpace(value) != "" {
		h.Set(key, value)
	}
}

func setIntHeader(h http.Header, key string, value int64) {
	h.Set(key, strconv.FormatInt(value, 10))
}
