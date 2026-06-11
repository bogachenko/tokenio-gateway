package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"request_id,omitempty"`
}

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
	ClientAmountCents           int64
	Currency                    string
	WalletBalanceCents          int64
	WalletEffectiveBalanceCents int64
	BillingPendingCents         int64
}

func ReadAllLimited(r *http.Request, limit int64) ([]byte, error) {
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("request body is too large")
	}
	return body, nil
}

func WriteError(w http.ResponseWriter, status int, code string, message string, requestID string) {
	WriteJSON(w, status, ErrorResponse{
		Error: ErrorBody{
			Code:      code,
			Message:   message,
			RequestID: requestID,
		},
	})
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func WriteRawResponse(w http.ResponseWriter, status int, header http.Header, body []byte) {
	CopyResponseHeaders(w.Header(), header)
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func CopyResponseHeaders(dst http.Header, src http.Header) {
	for key, values := range src {
		lower := strings.ToLower(key)
		switch lower {
		case "content-length", "connection", "transfer-encoding", "upgrade", "proxy-authenticate", "proxy-authorization", "te", "trailer":
			continue
		}
		for _, value := range values {
			dst.Add(key, value)
		}
	}
}

func SetBillingHeaders(h http.Header, headers BillingHeaders) {
	setHeader(h, "X-Local-Request-ID", headers.LocalRequestID)
	setHeader(h, "X-Billing-Provider-Type", string(headers.ProviderType))
	setHeader(h, "X-Billing-Client-Model", headers.ClientModel)
	setHeader(h, "X-Billing-Model", headers.BillingModel)
	setHeaderInt(h, "X-Billing-Input-Tokens", headers.InputTokens)
	setHeaderInt(h, "X-Billing-Cached-Input-Tokens", headers.CachedInputTokens)
	setHeaderInt(h, "X-Billing-Output-Tokens", headers.OutputTokens)
	setHeaderInt(h, "X-Billing-Reasoning-Tokens", headers.ReasoningTokens)
	setHeaderInt(h, "X-Billing-Image-Input-Tokens", headers.ImageInputTokens)
	setHeaderInt(h, "X-Billing-Audio-Input-Tokens", headers.AudioInputTokens)
	setHeaderInt(h, "X-Billing-Audio-Output-Tokens", headers.AudioOutputTokens)
	setHeaderInt(h, "X-Billing-File-Input-Tokens", headers.FileInputTokens)
	setHeaderInt(h, "X-Billing-Video-Input-Tokens", headers.VideoInputTokens)
	setHeaderInt(h, "X-Billing-Amount-Cents", headers.ClientAmountCents)
	setHeader(h, "X-Billing-Currency", headers.Currency)
	setHeaderInt(h, "X-Wallet-Balance-Cents", headers.WalletBalanceCents)
	setHeaderInt(h, "X-Wallet-Effective-Balance-Cents", headers.WalletEffectiveBalanceCents)
	setHeaderInt(h, "X-Billing-Pending-Cents", headers.BillingPendingCents)
}

func setHeader(h http.Header, key string, value string) {
	if strings.TrimSpace(value) != "" {
		h.Set(key, value)
	}
}

func setHeaderInt(h http.Header, key string, value int64) {
	h.Set(key, fmt.Sprintf("%d", value))
}
