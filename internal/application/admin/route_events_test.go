package admin

import (
	"context"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type routeEventStoreFake struct {
	ports.RouteEventStore
	filter ports.RouteEventListFilter
	page   ports.Page[domain.RouteEvent]
}

func (f *routeEventStoreFake) ListRouteEvents(
	_ context.Context,
	filter ports.RouteEventListFilter,
) (ports.Page[domain.RouteEvent], error) {
	f.filter = filter
	return f.page, nil
}

func TestAdminListRouteEventsUsesCanonicalStoreAndPagination(t *testing.T) {
	at := time.Date(2026, time.June, 18, 10, 0, 0, 0, time.UTC)
	store := &routeEventStoreFake{
		page: ports.Page[domain.RouteEvent]{
			Items: []domain.RouteEvent{{
				ID:        "event_1",
				RouteID:   "route_1",
				EventType: domain.RouteEventTypeSelected,
				CreatedAt: at,
			}},
			Total: 1,
		},
	}
	service := &Service{deps: Dependencies{RouteEvents: store}}
	result, err := service.ListRouteEvents(
		context.Background(),
		RouteEventListInput{
			RouteID:   "route_1",
			EventType: domain.RouteEventTypeSelected,
			Limit:     25,
			Offset:    5,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Data) != 1 ||
		result.Pagination.Limit != 25 ||
		result.Pagination.Offset != 5 ||
		result.Pagination.Total != 1 ||
		store.filter.RouteID != "route_1" ||
		store.filter.EventType != domain.RouteEventTypeSelected {
		t.Fatalf("result=%+v filter=%+v", result, store.filter)
	}
}
