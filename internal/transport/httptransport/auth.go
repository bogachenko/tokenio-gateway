package httptransport

import (
	"context"
	"errors"
	"net/http"

	"github.com/bogachenko/tokenio-gateway/internal/application/authenticate"
	"github.com/bogachenko/tokenio-gateway/internal/auth"
	"github.com/bogachenko/tokenio-gateway/internal/domain"
)

type PublicAuthenticator interface {
	AuthenticatePublicRequest(ctx context.Context, input authenticate.Input) (authenticate.Result, error)
}

type LocalRequestIDGenerator interface {
	NewLocalRequestID() string
}

type LocalRequestIDGeneratorFunc func() string

func (f LocalRequestIDGeneratorFunc) NewLocalRequestID() string {
	return f()
}

type PublicAuthMiddleware struct {
	authenticator PublicAuthenticator
	requestIDs    LocalRequestIDGenerator
}

func NewPublicAuthMiddleware(authenticator PublicAuthenticator, requestIDs LocalRequestIDGenerator) (*PublicAuthMiddleware, error) {
	if authenticator == nil {
		return nil, errors.New("public authenticator is required")
	}
	if requestIDs == nil {
		return nil, errors.New("local request id generator is required")
	}
	return &PublicAuthMiddleware{authenticator: authenticator, requestIDs: requestIDs}, nil
}

func (m *PublicAuthMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := m.requestIDs.NewLocalRequestID()
		w.Header().Set("X-Local-Request-ID", requestID)
		ctx := contextWithRequestID(r.Context(), requestID)
		if requestID == "" {
			WriteGatewayError(w, internalGatewayError(requestID))
			return
		}

		rawAPIKey, err := auth.ExtractBearerAPIKey(r.Header.Get("Authorization"))
		if err != nil {
			WriteGatewayError(w, mapAuthHeaderError(err, requestID))
			return
		}

		result, err := m.authenticator.AuthenticatePublicRequest(ctx, authenticate.Input{RawAPIKey: rawAPIKey})
		if err != nil {
			WriteGatewayError(w, mapAuthenticateError(err, requestID))
			return
		}

		downstream := r.Clone(contextWithPrincipal(ctx, result.Principal))
		downstream.Header.Del("Authorization")
		next.ServeHTTP(w, downstream)
	})
}

func mapAuthHeaderError(err error, requestID string) GatewayError {
	switch {
	case errors.Is(err, auth.ErrAuthorizationHeaderRequired):
		return GatewayError{Status: http.StatusUnauthorized, Code: domain.ErrorCodeUnauthorized, Message: "Authorization header is required", RequestID: requestID}
	case errors.Is(err, auth.ErrAuthorizationHeaderScheme):
		return GatewayError{Status: http.StatusUnauthorized, Code: domain.ErrorCodeUnauthorized, Message: "Authorization header format must be Bearer {api_key}", RequestID: requestID}
	case errors.Is(err, auth.ErrBearerAPIKeyEmpty):
		return GatewayError{Status: http.StatusUnauthorized, Code: domain.ErrorCodeUnauthorized, Message: "Bearer API key is empty", RequestID: requestID}
	case errors.Is(err, auth.ErrBearerAPIKeyPrefix):
		return GatewayError{Status: http.StatusUnauthorized, Code: domain.ErrorCodeUnauthorized, Message: "API key must start with sk_", RequestID: requestID}
	default:
		return internalGatewayError(requestID)
	}
}

func mapAuthenticateError(err error, requestID string) GatewayError {
	switch {
	case errors.Is(err, authenticate.ErrInvalidAPIKey):
		return GatewayError{Status: http.StatusUnauthorized, Code: domain.ErrorCodeInvalidAPIKey, Message: "Invalid API key", RequestID: requestID}
	case errors.Is(err, authenticate.ErrUserDisabled):
		return GatewayError{Status: http.StatusForbidden, Code: domain.ErrorCodeUserDisabled, Message: "User is disabled", RequestID: requestID}
	case errors.Is(err, authenticate.ErrInvalidIdentity):
		return internalGatewayError(requestID)
	default:
		return internalGatewayError(requestID)
	}
}
