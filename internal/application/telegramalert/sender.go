package telegramalert

import "github.com/bogachenko/tokenio-gateway/internal/ports/telegramalertdelivery"

type MessageDeliveryOutcome = telegramalertdelivery.MessageDeliveryOutcome

const (
	MessageDeliveryOutcomeNotSent          = telegramalertdelivery.MessageDeliveryOutcomeNotSent
	MessageDeliveryOutcomeSentNoResponse   = telegramalertdelivery.MessageDeliveryOutcomeSentNoResponse
	MessageDeliveryOutcomeResponseReceived = telegramalertdelivery.MessageDeliveryOutcomeResponseReceived
)

type MessageDeliveryResult = telegramalertdelivery.MessageDeliveryResult

type MessageSender = telegramalertdelivery.MessageSender
