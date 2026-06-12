package httptransport

import (
	"encoding/json"
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

const internalServerErrorMessage = "Internal server error"

type ErrorResponse struct {
	Error ErrorBody `json:"error"`
}

type ErrorBody struct {
	Code      domain.ErrorCode `json:"code"`
	Message   string           `json:"message"`
	RequestID string           `json:"request_id,omitempty"`
}

type GatewayError struct {
	Status    int
	Code      domain.ErrorCode
	Message   string
	RequestID string
}

func WriteGatewayError(w http.ResponseWriter, gatewayErr GatewayError) {
	status := gatewayErr.Status
	if status == 0 {
		status = http.StatusInternalServerError
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(ErrorResponse{
		Error: ErrorBody{
			Code:      gatewayErr.Code,
			Message:   gatewayErr.Message,
			RequestID: gatewayErr.RequestID,
		},
	})
}

func internalGatewayError(requestID string) GatewayError {
	return GatewayError{
		Status:    http.StatusInternalServerError,
		Code:      domain.ErrorCodeInternalError,
		Message:   internalServerErrorMessage,
		RequestID: requestID,
	}
}
