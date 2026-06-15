package telegramalert

import "context"

// MessageSender delivers an already composed Telegram message.
// It does not decide whether an alert is required and does not mutate
// persisted alert lifecycle state.
type MessageSender interface {
	SendMessage(context.Context, string) error
}
