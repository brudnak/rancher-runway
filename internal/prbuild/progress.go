package prbuild

import (
	"context"

	"time"
)

type Progress struct {
	At      time.Time `json:"at"`
	Message string    `json:"message"`
}

type readinessProgressKey struct{}

func Notify(ctx context.Context, message string) {
	if callback, ok := ctx.Value(readinessProgressKey{}).(func(Progress)); ok && ctx.Err() == nil {
		callback(Progress{At: time.Now().UTC(), Message: message})
	}
}

func WithProgress(ctx context.Context, callback func(Progress)) context.Context {
	return context.WithValue(ctx, readinessProgressKey{}, callback)
}
