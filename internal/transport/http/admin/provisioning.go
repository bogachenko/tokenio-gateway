package adminhttp

import (
	"net/http"

	application "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func (h *Router) handleAPIKeyProvisionings(
	writer http.ResponseWriter,
	request *http.Request,
	command application.CommandContext,
) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, command.RequestID, http.MethodGet)
		return
	}

	page, ok := parsePage(writer, request, command.RequestID)
	if !ok {
		return
	}
	createdFrom, ok := parseOptionalTime(
		writer,
		request,
		command.RequestID,
		"created_from",
	)
	if !ok {
		return
	}
	createdTo, ok := parseOptionalTime(
		writer,
		request,
		command.RequestID,
		"created_to",
	)
	if !ok {
		return
	}

	query := request.URL.Query()
	result, err := h.service.ListAPIKeyProvisionings(
		request.Context(),
		application.APIKeyProvisioningListInput{
			ExternalBillingUserID: query.Get("external_billing_user_id"),
			UserID:                query.Get("user_id"),
			APIKeyID:              query.Get("api_key_id"),
			Status: domain.APIKeyProvisioningStatus(
				query.Get("status"),
			),
			ResultType: domain.APIKeyProvisioningResultType(
				query.Get("result_type"),
			),
			CreatedFrom: createdFrom,
			CreatedTo:   createdTo,
			Limit:       page.limit,
			Offset:      page.offset,
		},
	)
	if err != nil {
		writeApplicationError(writer, command.RequestID, err)
		return
	}
	writeList(writer, result.Data, result.Pagination)
}
