package domain

import "time"

type TelegramDeliveryAttemptStatus string

const (
	TelegramDeliveryAttemptStatusStarted   TelegramDeliveryAttemptStatus = "started"
	TelegramDeliveryAttemptStatusSucceeded TelegramDeliveryAttemptStatus = "succeeded"
	TelegramDeliveryAttemptStatusFailed    TelegramDeliveryAttemptStatus = "failed"
)

type TelegramDeliveryAttemptState string

const (
	TelegramDeliveryAttemptStateNotSent          TelegramDeliveryAttemptState = "not_sent"
	TelegramDeliveryAttemptStateSentNoResponse   TelegramDeliveryAttemptState = "sent_no_response"
	TelegramDeliveryAttemptStateResponseReceived TelegramDeliveryAttemptState = "response_received"
)

type TelegramDeliveryAttempt struct {
	ID string

	AlertID       string
	AttemptNumber int

	Status       TelegramDeliveryAttemptStatus
	AttemptState TelegramDeliveryAttemptState
	FailureCode  string

	TelegramMessageID string

	StartedAt   time.Time
	CompletedAt *time.Time
}
