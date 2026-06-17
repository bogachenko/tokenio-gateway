package httptransport

import (
	"net/http"
	"testing"

	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

func TestStatusForApplicationError(t *testing.T) {
	tests := []struct {
		category ports.FailureCategory
		status   int
	}{
		{ports.FailureCategoryInvalidRequest, http.StatusBadRequest},
		{ports.FailureCategoryUnauthorized, http.StatusUnauthorized},
		{ports.FailureCategoryForbidden, http.StatusForbidden},
		{ports.FailureCategoryPaymentRequired, http.StatusPaymentRequired},
		{ports.FailureCategoryConflict, http.StatusConflict},
		{ports.FailureCategoryGone, http.StatusGone},
		{ports.FailureCategoryDependencyUnavailable, http.StatusBadGateway},
		{ports.FailureCategoryUnavailable, http.StatusServiceUnavailable},
		{ports.FailureCategoryInternal, http.StatusInternalServerError},
		{ports.FailureCategory("unknown"), http.StatusInternalServerError},
	}
	for _, test := range tests {
		t.Run(string(test.category), func(t *testing.T) {
			got := StatusForApplicationError(&ports.ApplicationError{Category: test.category})
			if got != test.status {
				t.Fatalf("status = %d, want %d", got, test.status)
			}
		})
	}
	if got := StatusForApplicationError(nil); got != http.StatusInternalServerError {
		t.Fatalf("nil status = %d", got)
	}
}
