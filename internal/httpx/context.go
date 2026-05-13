package httpx

import "context"

type ctxKey int

const (
	requestIDKey ctxKey = iota
	userIDKey
	workspaceIDKey
	workspaceRoleKey
)

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey, id)
}

func RequestID(ctx context.Context) string {
	v, _ := ctx.Value(requestIDKey).(string)
	return v
}

func WithUserID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, userIDKey, id)
}

func UserID(ctx context.Context) string {
	v, _ := ctx.Value(userIDKey).(string)
	return v
}

func WithWorkspaceID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, workspaceIDKey, id)
}

func WorkspaceID(ctx context.Context) string {
	v, _ := ctx.Value(workspaceIDKey).(string)
	return v
}

func WithWorkspaceRole(ctx context.Context, role string) context.Context {
	return context.WithValue(ctx, workspaceRoleKey, role)
}

func WorkspaceRole(ctx context.Context) string {
	v, _ := ctx.Value(workspaceRoleKey).(string)
	return v
}
