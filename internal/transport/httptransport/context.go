package httptransport

import (
	"context"

	"github.com/bogachenko/tokenio-gateway/internal/auth"
)

type requestIDContextKey struct{}
type principalContextKey struct{}

func contextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, requestIDContextKey{}, requestID)
}

func RequestIDFromContext(ctx context.Context) (string, bool) {
	requestID, ok := ctx.Value(requestIDContextKey{}).(string)
	return requestID, ok && requestID != ""
}

func contextWithPrincipal(ctx context.Context, principal auth.APIKeyPrincipal) context.Context {
	return context.WithValue(ctx, principalContextKey{}, principal)
}

func PrincipalFromContext(ctx context.Context) (auth.APIKeyPrincipal, bool) {
	principal, ok := ctx.Value(principalContextKey{}).(auth.APIKeyPrincipal)
	return principal, ok
}
