//go:build !darwin && !dragonfly && !freebsd && !linux && !netbsd && !openbsd && !solaris && !aix && !windows
// +build !darwin,!dragonfly,!freebsd,!linux,!netbsd,!openbsd,!solaris,!aix,!windows

package uv

import (
	"context"
	"os"
	"os/signal"
)

func openTTY() (*os.File, *os.File, error) {
	return nil, nil, ErrPlatformNotSupported
}

func suspend() error {
	return ErrPlatformNotSupported
}

func notifyWinch(c chan os.Signal, sigs ...os.Signal) {
	signal.Notify(c, sigs...)
}

func notifyWinchContext(ctx context.Context, sigs ...os.Signal) (context.Context, context.CancelFunc) {
	return signal.NotifyContext(ctx, sigs...)
}
