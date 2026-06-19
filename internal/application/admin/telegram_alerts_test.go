package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

func TestListTelegramAlerts(t *testing.T) {
	service := newServiceForTest(
		t,
		&fakeUsers{},
		&fakeKeys{},
		&fakeResellers{},
		&fakeRoutes{},
		&fakeLedger{},
		&fakeSecrets{},
		&fakeGenerator{},
	)
	alerts := service.deps.TelegramAlerts.(*telegramAlertStoreFake)
	createdFrom := time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
	createdTo := createdFrom.Add(time.Hour)
	alerts.page.Items = []domain.TelegramAlert{
		{
			ID:         "tgalt_1",
			AlertType:  "reseller_balance_low",
			DedupeKey:  "reseller_1",
			ResellerID: "reseller_1",
			Message:    "low",
			Status:     domain.TelegramAlertStatusFailed,
			Error:      "send failed",
			CreatedAt:  createdFrom,
		},
	}
	alerts.page.Total = 1

	result, err := service.ListTelegramAlerts(
		context.Background(),
		TelegramAlertListInput{
			AlertType:   "reseller_balance_low",
			ResellerID:  "reseller_1",
			Status:      domain.TelegramAlertStatusFailed,
			CreatedFrom: &createdFrom,
			CreatedTo:   &createdTo,
			Limit:       25,
			Offset:      50,
		},
	)
	if err != nil {
		t.Fatalf("ListTelegramAlerts: %v", err)
	}

	if alerts.filter.AlertType != "reseller_balance_low" ||
		alerts.filter.ResellerID != "reseller_1" ||
		alerts.filter.Status != domain.TelegramAlertStatusFailed ||
		alerts.filter.CreatedFrom != &createdFrom ||
		alerts.filter.CreatedTo != &createdTo ||
		alerts.filter.Page.Limit != 25 ||
		alerts.filter.Page.Offset != 50 {
		t.Fatalf("filter = %#v", alerts.filter)
	}
	if result.Pagination.Total != 1 ||
		len(result.Data) != 1 ||
		result.Data[0].ID != "tgalt_1" ||
		result.Data[0].Error != "send failed" {
		t.Fatalf("result = %#v", result)
	}
}

func TestListTelegramAlertsRejectsInvalidStatus(t *testing.T) {
	service := newServiceForTest(
		t,
		&fakeUsers{},
		&fakeKeys{},
		&fakeResellers{},
		&fakeRoutes{},
		&fakeLedger{},
		&fakeSecrets{},
		&fakeGenerator{},
	)
	_, err := service.ListTelegramAlerts(
		context.Background(),
		TelegramAlertListInput{
			Status: "bad_status",
			Limit:  25,
		},
	)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error = %v, want ErrInvalidRequest", err)
	}
}

func TestRetryTelegramAlertMovesFailedAlertToPendingWithAudit(t *testing.T) {
	service := newServiceForTest(
		t,
		&fakeUsers{},
		&fakeKeys{},
		&fakeResellers{},
		&fakeRoutes{},
		&fakeLedger{},
		&fakeSecrets{},
		&fakeGenerator{},
	)
	alerts := service.deps.TelegramAlerts.(*telegramAlertStoreFake)
	createdAt := time.Date(2026, 6, 18, 10, 0, 0, 0, time.UTC)
	alerts.alert = &domain.TelegramAlert{
		ID:         "tgalt_1",
		AlertType:  "reseller_balance_low",
		DedupeKey:  "reseller_1",
		ResellerID: "reseller_1",
		Message:    "low",
		Status:     domain.TelegramAlertStatusFailed,
		Error:      "telegram_unavailable",
		CreatedAt:  createdAt,
	}

	result, err := service.RetryTelegramAlert(
		context.Background(),
		command(),
		"tgalt_1",
		"manual operator retry",
	)
	if err != nil {
		t.Fatalf("RetryTelegramAlert: %v", err)
	}
	if result.ID != "tgalt_1" ||
		result.Status != domain.TelegramAlertStatusPending ||
		result.Error != "" ||
		result.SentAt != nil {
		t.Fatalf("result = %#v", result)
	}
	if alerts.retryExpected.Status != domain.TelegramAlertStatusFailed ||
		alerts.retryNext.Status != domain.TelegramAlertStatusPending ||
		alerts.retryNext.Error != "" ||
		alerts.retryNext.SentAt != nil {
		t.Fatalf(
			"expected=%#v next=%#v",
			alerts.retryExpected,
			alerts.retryNext,
		)
	}
	if alerts.retryAudit.Action != domain.AuditActionTelegramAlertRetry ||
		alerts.retryAudit.EntityType != "telegram_alert" ||
		alerts.retryAudit.EntityID != "tgalt_1" ||
		alerts.retryAudit.RequestID != command().RequestID ||
		alerts.retryAudit.Reason != "manual operator retry" {
		t.Fatalf("audit = %#v", alerts.retryAudit)
	}
}

func TestRetryTelegramAlertRejectsNonFailedAlert(t *testing.T) {
	service := newServiceForTest(
		t,
		&fakeUsers{},
		&fakeKeys{},
		&fakeResellers{},
		&fakeRoutes{},
		&fakeLedger{},
		&fakeSecrets{},
		&fakeGenerator{},
	)
	alerts := service.deps.TelegramAlerts.(*telegramAlertStoreFake)
	alerts.alert = &domain.TelegramAlert{
		ID:        "tgalt_1",
		AlertType: "reseller_balance_low",
		DedupeKey: "reseller_1",
		Message:   "low",
		Status:    domain.TelegramAlertStatusPending,
		CreatedAt: time.Date(
			2026, 6, 18, 10, 0, 0, 0, time.UTC,
		),
	}

	_, err := service.RetryTelegramAlert(
		context.Background(),
		command(),
		"tgalt_1",
		"manual operator retry",
	)
	if !errors.Is(err, ErrStateConflict) {
		t.Fatalf("error = %v, want ErrStateConflict", err)
	}
}

func TestRetryTelegramAlertRejectsBlankReason(t *testing.T) {
	service := newServiceForTest(
		t,
		&fakeUsers{},
		&fakeKeys{},
		&fakeResellers{},
		&fakeRoutes{},
		&fakeLedger{},
		&fakeSecrets{},
		&fakeGenerator{},
	)

	_, err := service.RetryTelegramAlert(
		context.Background(),
		command(),
		"tgalt_1",
		" ",
	)
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("error = %v, want ErrInvalidRequest", err)
	}
}
