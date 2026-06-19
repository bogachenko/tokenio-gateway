package adminhttp

import (
	"net/http"
	"strings"

	application "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func (h *Router) handleTelegramAlerts(
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
		writer, request, command.RequestID, "created_from",
	)
	if !ok {
		return
	}
	createdTo, ok := parseOptionalTime(
		writer, request, command.RequestID, "created_to",
	)
	if !ok {
		return
	}
	query := request.URL.Query()
	result, err := h.service.ListTelegramAlerts(
		request.Context(),
		application.TelegramAlertListInput{
			AlertType:   query.Get("alert_type"),
			ResellerID:  query.Get("reseller_id"),
			Status:      domain.TelegramAlertStatus(query.Get("status")),
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

func (h *Router) handleTelegramAlertPath(
	writer http.ResponseWriter,
	request *http.Request,
	command application.CommandContext,
	parts []string,
) {
	if len(parts) != 2 || parts[1] != "retry" {
		writeError(writer, command.RequestID, http.StatusNotFound, domain.ErrorCodeNotFound, "Endpoint not found")
		return
	}
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, command.RequestID, http.MethodPost)
		return
	}
	var dto reasonDTO
	if !decodeJSON(writer, request, command.RequestID, &dto) {
		return
	}
	dto.Reason = strings.TrimSpace(dto.Reason)
	if dto.Reason == "" {
		writeAdminValidationError(writer, command.RequestID)
		return
	}
	result, err := h.service.RetryTelegramAlert(request.Context(), command, parts[0], dto.Reason)
	if err != nil {
		writeApplicationError(writer, command.RequestID, err)
		return
	}
	writeData(writer, result)
}
