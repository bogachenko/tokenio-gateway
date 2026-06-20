package ollamanative

import (
	"errors"

	"github.com/bogachenko/tokenio-gateway/internal/infrastructure/forwarding/bodyjson"
)

func replaceTopModelBytes(body []byte, clientModel, providerModel string) ([]byte, error) {
	out, err := bodyjson.ReplaceTopLevelString(body, "model", clientModel, providerModel)
	if err == nil {
		return out, nil
	}
	if errors.Is(err, bodyjson.ErrMismatch) {
		return nil, ErrUnsupportedRoute
	}
	return nil, ErrInvalidForwardRequest
}
