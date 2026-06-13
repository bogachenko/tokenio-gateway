package domain

import "time"

type BillingSession struct {
	UserID               string
	BillingSubjectUserID string

	RemoteBalanceCents       int64
	PendingAmountCentsCached int64
	Currency                 string

	FetchedAt time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

type RouteEventType string

const (
	RouteEventTypeSelected             RouteEventType = "selected"
	RouteEventTypeSkipped              RouteEventType = "skipped"
	RouteEventTypeCooldownSet          RouteEventType = "cooldown_set"
	RouteEventTypeCooldownExpired      RouteEventType = "cooldown_expired"
	RouteEventTypeRetry                RouteEventType = "retry"
	RouteEventTypeFailure              RouteEventType = "failure"
	RouteEventTypeSuccess              RouteEventType = "success"
	RouteEventTypeHealthcheckFailed    RouteEventType = "healthcheck_failed"
	RouteEventTypeHealthcheckRecovered RouteEventType = "healthcheck_recovered"
	RouteEventTypeBalanceLow           RouteEventType = "balance_low"
)

type RouteEventMetadata map[string]any

type RouteEvent struct {
	ID string

	RouteID    string
	ResellerID string

	ProviderType ProviderType
	APIFamily    APIFamily
	EndpointKind EndpointKind
	ClientModel  string

	EventType      RouteEventType
	Reason         string
	LocalRequestID string

	Metadata  RouteEventMetadata
	CreatedAt time.Time
}

type TelegramAlertStatus string

const (
	TelegramAlertStatusPending    TelegramAlertStatus = "pending"
	TelegramAlertStatusSent       TelegramAlertStatus = "sent"
	TelegramAlertStatusFailed     TelegramAlertStatus = "failed"
	TelegramAlertStatusSuppressed TelegramAlertStatus = "suppressed"
)

type TelegramAlert struct {
	ID string

	AlertType string
	DedupeKey string

	ResellerID string
	RouteID    string

	Message string
	Status  TelegramAlertStatus
	Error   string

	CreatedAt time.Time
	SentAt    *time.Time
}
