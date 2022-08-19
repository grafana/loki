package cmdutil

import (
	"os"
	"os/signal"
	"syscall"
)

// WatchSignals blocks the current goroutine / thread and waits for any signal and returns an error
func WatchSignals() os.Signal {
	// Set up channel on which to send signal notifications.
	// We must use a buffered channel or risk missing the signal
	// if we're not ready to receive when the signal is sent.
	c := make(chan os.Signal, 1)

	NotifySignals(c)

	sig := <-c
	return sig
}

func NotifySignals(c chan os.Signal) {
	// Passing no signals to Notify means that
	// all signals will be sent to the channel.
	signal.Notify(c,
		os.Interrupt,
		syscall.SIGINT,
		syscall.SIGTERM,
		syscall.SIGQUIT,
	)
}
