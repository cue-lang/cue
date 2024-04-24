package httplog

import (
	"context"
	"log/slog"
)

type SlogLogger struct {
	Logger *slog.Logger
	Level  slog.Level
}

func (l SlogLogger) Log(ctx context.Context, kind EventKind, r RequestOrResponse) {
	logger := l.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// TODO this does not format well when using slog.TextHandler.
	// Is there anything we can do about that?
	logger.Log(ctx, l.Level, kind.String(), "info", r)
}
