package admin

import (
	"context"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
	"github.com/bogachenko/tokenio-gateway/internal/ports"
)

type TelegramAlertListInput struct {
	AlertType   string
	ResellerID  string
	Status      domain.TelegramAlertStatus
	CreatedFrom *time.Time
	CreatedTo   *time.Time
	Limit       int
	Offset      int
}

type TelegramAlertView struct {
	ID         string                     `json:"id"`
	AlertType  string                     `json:"alert_type"`
	DedupeKey  string                     `json:"dedupe_key"`
	ResellerID string                     `json:"reseller_id"`
	RouteID    string                     `json:"route_id"`
	Message    string                     `json:"message"`
	Status     domain.TelegramAlertStatus `json:"status"`
	Error      string                     `json:"error"`
	CreatedAt  time.Time                  `json:"created_at"`
	SentAt     *time.Time                 `json:"sent_at"`
}

func telegramAlertView(alert domain.TelegramAlert) TelegramAlertView {
	return TelegramAlertView{
		ID:         alert.ID,
		AlertType:  alert.AlertType,
		DedupeKey:  alert.DedupeKey,
		ResellerID: alert.ResellerID,
		RouteID:    alert.RouteID,
		Message:    alert.Message,
		Status:     alert.Status,
		Error:      alert.Error,
		CreatedAt:  alert.CreatedAt,
		SentAt:     alert.SentAt,
	}
}

func telegramAlertViews(alerts []domain.TelegramAlert) []TelegramAlertView {
	views := make([]TelegramAlertView, 0, len(alerts))
	for _, alert := range alerts {
		views = append(views, telegramAlertView(alert))
	}
	return views
}

func validTelegramAlertStatus(status domain.TelegramAlertStatus) bool {
	switch status {
	case "",
		domain.TelegramAlertStatusPending,
		domain.TelegramAlertStatusSent,
		domain.TelegramAlertStatusFailed,
		domain.TelegramAlertStatusSuppressed:
		return true
	default:
		return false
	}
}

func (s *Service) ListTelegramAlerts(
	ctx context.Context,
	input TelegramAlertListInput,
) (ListResult[TelegramAlertView], error) {
	pageReq, err := normalizePage(input.Limit, input.Offset)
	if err != nil {
		return ListResult[TelegramAlertView]{}, err
	}
	if err := validateWindow(input.CreatedFrom, input.CreatedTo); err != nil {
		return ListResult[TelegramAlertView]{}, err
	}
	if !validTelegramAlertStatus(input.Status) {
		return ListResult[TelegramAlertView]{}, ErrInvalidRequest
	}

	page, err := s.deps.TelegramAlerts.ListTelegramAlerts(
		ctx,
		ports.TelegramAlertListFilter{
			AlertType:   input.AlertType,
			ResellerID:  input.ResellerID,
			Status:      input.Status,
			CreatedFrom: input.CreatedFrom,
			CreatedTo:   input.CreatedTo,
			Page:        pageReq,
		},
	)
	if err != nil {
		return ListResult[TelegramAlertView]{}, mapStoreError(err)
	}

	return ListResult[TelegramAlertView]{
		Data: telegramAlertViews(page.Items),
		Pagination: Pagination{
			Limit:  pageReq.Limit,
			Offset: pageReq.Offset,
			Total:  page.Total,
		},
	}, nil
}

func (s *Service) RetryTelegramAlert(
	ctx context.Context,
	command CommandContext,
	alertID string,
	reason string,
) (TelegramAlertView, error) {
	if ctx == nil || s == nil || s.deps.TelegramAlerts == nil || s.deps.Clock == nil {
		return TelegramAlertView{}, ErrInvalidRequest
	}
	if err := ctx.Err(); err != nil {
		return TelegramAlertView{}, err
	}
	if err := validateCommand(command); err != nil {
		return TelegramAlertView{}, err
	}
	if isBlank(alertID) || isBlank(reason) {
		return TelegramAlertView{}, ErrInvalidRequest
	}

	current, err := s.deps.TelegramAlerts.FindTelegramAlertByID(ctx, alertID)
	if err != nil {
		return TelegramAlertView{}, mapStoreError(err)
	}
	if current == nil || current.ID != alertID {
		return TelegramAlertView{}, ErrNotFound
	}
	if current.Status != domain.TelegramAlertStatusFailed {
		return TelegramAlertView{}, ErrStateConflict
	}

	next := *current
	next.Status = domain.TelegramAlertStatusPending
	next.Error = ""
	next.SentAt = nil

	now, err := nowUTC(s.deps.Clock)
	if err != nil {
		return TelegramAlertView{}, err
	}
	persisted, err := s.deps.TelegramAlerts.CompareAndSwapTelegramAlertWithAudit(
		ctx,
		*current,
		next,
		auditContextWithReason(
			command,
			domain.AuditActionTelegramAlertRetry,
			"telegram_alert",
			current.ID,
			telegramAlertAdminState(*current),
			telegramAlertAdminState(next),
			reason,
			now,
		),
	)
	if err != nil {
		return TelegramAlertView{}, mapStoreError(err)
	}
	if persisted.Status != domain.TelegramAlertStatusPending ||
		persisted.Error != "" ||
		persisted.SentAt != nil {
		return TelegramAlertView{}, ErrStoreUnavailable
	}
	return telegramAlertView(persisted), nil
}

func telegramAlertAdminState(value domain.TelegramAlert) domain.AuditState {
	return domain.AuditState{
		"id":          value.ID,
		"alert_type":  value.AlertType,
		"dedupe_key":  value.DedupeKey,
		"reseller_id": value.ResellerID,
		"route_id":    value.RouteID,
		"message":     value.Message,
		"status":      value.Status,
		"error":       value.Error,
		"created_at":  value.CreatedAt.UTC(),
		"sent_at":     value.SentAt,
	}
}
