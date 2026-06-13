package ctxutil

import "context"

type userKey struct{}

type User struct {
	ID        string
	Role      string
	SessionID string
}

func WithUser(ctx context.Context, u User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

func UserFrom(ctx context.Context) (User, bool) {
	u, ok := ctx.Value(userKey{}).(User)
	return u, ok
}
