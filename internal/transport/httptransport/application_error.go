package httptransport

import (
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func StatusForApplicationError(applicationError *ports.ApplicationError) int {
	if applicationError == nil {
		return http.StatusInternalServerError
	}
	switch applicationError.Category {
	case ports.FailureCategoryInvalidRequest:
		return http.StatusBadRequest
	case ports.FailureCategoryUnauthorized:
		return http.StatusUnauthorized
	case ports.FailureCategoryForbidden:
		return http.StatusForbidden
	case ports.FailureCategoryPaymentRequired:
		return http.StatusPaymentRequired
	case ports.FailureCategoryConflict:
		return http.StatusConflict
	case ports.FailureCategoryNotFound:
		return http.StatusNotFound
	case ports.FailureCategoryGone:
		return http.StatusGone
	case ports.FailureCategoryDependencyUnavailable:
		return http.StatusBadGateway
	case ports.FailureCategoryUnavailable:
		return http.StatusServiceUnavailable
	default:
		return http.StatusInternalServerError
	}
}
