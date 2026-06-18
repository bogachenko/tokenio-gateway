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
