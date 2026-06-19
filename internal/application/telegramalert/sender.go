package telegramalert

import "context"

type MessageDeliveryOutcome string

const (
	MessageDeliveryOutcomeNotSent          MessageDeliveryOutcome = "not_sent"
	MessageDeliveryOutcomeSentNoResponse   MessageDeliveryOutcome = "sent_no_response"
	MessageDeliveryOutcomeResponseReceived MessageDeliveryOutcome = "response_received"
)

type MessageDeliveryResult struct {
	Outcome           MessageDeliveryOutcome
	TelegramMessageID string
}

// MessageSender delivers an already composed Telegram message.
// Outcome deterministically describes how far the external side effect reached.
// The sender does not mutate persisted alert or attempt lifecycle state.
type MessageSender interface {
	SendMessage(
		context.Context,
		string,
	) (MessageDeliveryResult, error)
}
