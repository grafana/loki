package uv

import (
	"context"
	"os"
)

// OpenTTY opens the terminal's input and output file descriptors.
// It returns the input and output files, or an error if the terminal is not
// available.
//
// This is useful for applications that need to interact with the terminal
// directly while piping or redirecting input/output.
func OpenTTY() (inTty, outTty *os.File, err error) {
	return openTTY()
}

// Suspend suspends the current process group.
func Suspend() error {
	return suspend()
}

// NotifyWinch sets up a channel to receive window size change signals and any
// other signals needed. This is a drop-in replacement for os/signal.Notify to
// ensure that SIGWINCH is included.
//
// On Windows, this will be a no-op for SIGWINCH, but other signals may still
// be handled.
func NotifyWinch(c chan os.Signal, sigs ...os.Signal) {
	notifyWinch(c, sigs...)
}

// NotifyWinchContext sets up a channel to receive window size change signals
// and any other signals needed, with context cancellation support. This is a
// drop-in replacement for os/signal.NotifyContext to ensure that SIGWINCH is
// included.
//
// On Windows, this will be a no-op for SIGWINCH, but other signals may still
// be handled.
func NotifyWinchContext(ctx context.Context, sigs ...os.Signal) (context.Context, context.CancelFunc) {
	return notifyWinchContext(ctx, sigs...)
}
