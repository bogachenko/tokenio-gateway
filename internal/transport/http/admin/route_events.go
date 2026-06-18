package adminhttp

import (
	"net/http"

	application "github.com/bogachenko/tokenio-gateway/internal/application/admin"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func (h *Router) handleRouteEvents(
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
	result, err := h.service.ListRouteEvents(
		request.Context(),
		application.RouteEventListInput{
			RouteID:        query.Get("route_id"),
			ResellerID:     query.Get("reseller_id"),
			EventType:      domain.RouteEventType(query.Get("event_type")),
			LocalRequestID: query.Get("local_request_id"),
			CreatedFrom:    createdFrom,
			CreatedTo:      createdTo,
			Limit:          page.limit,
			Offset:         page.offset,
		},
	)
	if err != nil {
		writeApplicationError(writer, command.RequestID, err)
		return
	}
	writeList(writer, result.Data, result.Pagination)
}
