package apicontext

import "context"

type contextKey string

const authInfoKey contextKey = "auth_info"

type AuthInfo struct {
	TokenID   string
	TokenName string
	Scope     string
	Source    string
	SessionID string
}

func WithAuthInfo(ctx context.Context, info *AuthInfo) context.Context {
	return context.WithValue(ctx, authInfoKey, info)
}

func AuthInfoFromContext(ctx context.Context) (*AuthInfo, bool) {
	info, ok := ctx.Value(authInfoKey).(*AuthInfo)
	return info, ok
}
