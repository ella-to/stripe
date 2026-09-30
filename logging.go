package stripe

import (
	"context"
	"fmt"
	"log/slog"
)

// WithLogger sets the slog.Logger used by the client: stripe-go's internal
// request logging goes through it, and webhook Dispatchers created with
// Client.Webhooks report rejected deliveries and handler errors to it.
// Defaults to slog.Default().
//
// stripe-go's own messages are logged at Debug level (request errors are
// returned to you anyway), except warnings, which stay at Warn.
func WithLogger(l *slog.Logger) Option {
	return func(c *Client) { c.logger = l }
}

// slogAdapter implements stripe-go's ContextLeveledLoggerInterface on top of
// slog. The logger is resolved on every call so that a later slog.SetDefault
// is honoured when no explicit logger was configured.
type slogAdapter struct {
	logger func() *slog.Logger
}

func (a slogAdapter) log(ctx context.Context, level slog.Level, format string, v ...interface{}) {
	if ctx == nil {
		ctx = context.Background()
	}
	l := a.logger()
	if !l.Enabled(ctx, level) {
		return
	}
	l.Log(ctx, level, "stripe-go: "+fmt.Sprintf(format, v...))
}

func (a slogAdapter) Debugf(ctx context.Context, format string, v ...interface{}) {
	a.log(ctx, slog.LevelDebug, format, v...)
}

func (a slogAdapter) Infof(ctx context.Context, format string, v ...interface{}) {
	a.log(ctx, slog.LevelDebug, format, v...)
}

func (a slogAdapter) Warnf(ctx context.Context, format string, v ...interface{}) {
	a.log(ctx, slog.LevelWarn, format, v...)
}

func (a slogAdapter) Errorf(ctx context.Context, format string, v ...interface{}) {
	a.log(ctx, slog.LevelDebug, format, v...)
}
