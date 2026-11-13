package memlimit

import (
	"context"
	"log/slog"
)

var _ slog.Handler = discardHandler{}

type discardHandler struct{}

func (discardHandler) Enabled(context.Context, slog.Level) bool  { return false }
func (discardHandler) Handle(context.Context, slog.Record) error { return nil }
func (dh discardHandler) WithAttrs([]slog.Attr) slog.Handler     { return dh }
func (dh discardHandler) WithGroup(string) slog.Handler          { return dh }

func memlimitLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.New(discardHandler{})
	}
	return logger.With(slog.String("package", "github.com/KimMachineGun/automemlimit/memlimit"))
}
