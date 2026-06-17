package ports

import (
	"context"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type BillingSessionStore interface {
	FindBillingSessionByUserID(
		context.Context,
		string,
	) (*domain.BillingSession, error)

	// UpsertBillingSession creates when expected is nil. Otherwise it performs
	// an exact compare-and-swap against the persisted session.
	UpsertBillingSession(
		context.Context,
		*domain.BillingSession,
		domain.BillingSession,
	) (domain.BillingSession, error)
}

type RouteEventListFilter struct {
	RouteID        string
	ResellerID     string
	EventType      domain.RouteEventType
	LocalRequestID string
	CreatedFrom    *time.Time
	CreatedTo      *time.Time
	Page           PageRequest
}

type RouteCooldownStore interface {
	// CompareAndSwapRouteCooldownWithEvent atomically verifies the exact
	// persisted route snapshot, applies only operational cooldown fields,
	// and appends the corresponding cooldown_set event.
	CompareAndSwapRouteCooldownWithEvent(
		context.Context,
		domain.Route,
		domain.Route,
		domain.RouteEvent,
	) (domain.Route, error)
}

type RouteEventStore interface {
	// AppendRouteEvent is idempotent for the same event ID and exact payload.
	AppendRouteEvent(context.Context, domain.RouteEvent) error
	FindRouteEventByID(
		context.Context,
		string,
	) (*domain.RouteEvent, error)
	ListRouteEvents(
		context.Context,
		RouteEventListFilter,
	) (Page[domain.RouteEvent], error)
}

type TelegramAlertListFilter struct {
	AlertType   string
	ResellerID  string
	Status      domain.TelegramAlertStatus
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Page        PageRequest
}

type TelegramAlertStore interface {
	// CreateOrSuppressTelegramAlert atomically checks dedupe scope and inserts
	// either the requested pending alert or a suppressed history record.
	CreateOrSuppressTelegramAlert(
		context.Context,
		domain.TelegramAlert,
		time.Duration,
	) (domain.TelegramAlert, error)

	FindTelegramAlertByID(
		context.Context,
		string,
	) (*domain.TelegramAlert, error)

	ListTelegramAlerts(
		context.Context,
		TelegramAlertListFilter,
	) (Page[domain.TelegramAlert], error)

	// CompareAndSwapTelegramAlert applies an exact lifecycle transition.
	CompareAndSwapTelegramAlert(
		context.Context,
		domain.TelegramAlert,
		domain.TelegramAlert,
	) (domain.TelegramAlert, error)
}
