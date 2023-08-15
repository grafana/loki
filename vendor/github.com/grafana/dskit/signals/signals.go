// Provenance-includes-location: https://github.com/weaveworks/common/blob/main/signals/signals.go
// Provenance-includes-license: Apache-2.0
// Provenance-includes-copyright: Weaveworks Ltd.

package signals

import (
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/grafana/dskit/log"
)

// SignalReceiver represents a subsystem/server/... that can be stopped or
// queried about the status with a signal
type SignalReceiver interface {
	Stop() error
}

// Handler handles signals, can be interrupted.
// On SIGINT or SIGTERM it will exit, on SIGQUIT it
// will dump goroutine stacks to the Logger.
type Handler struct {
	log       log.Interface
	receivers []SignalReceiver
	quit      chan struct{}
}

// NewHandler makes a new Handler.
func NewHandler(log log.Interface, receivers ...SignalReceiver) *Handler {
	return &Handler{
		log:       log,
		receivers: receivers,
		quit:      make(chan struct{}),
	}
}

// Stop the handler
func (h *Handler) Stop() {
	close(h.quit)
}

// Loop handles signals.
func (h *Handler) Loop() {
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM)
	defer signal.Stop(sigs)
	buf := make([]byte, 1<<20)
	for {
		select {
		case <-h.quit:
			h.log.Infof("=== Handler.Stop()'d ===")
			return
		case sig := <-sigs:
			switch sig {
			case syscall.SIGINT, syscall.SIGTERM:
				h.log.Infof("=== received SIGINT/SIGTERM ===\n*** exiting")
				for _, subsystem := range h.receivers {
					_ = subsystem.Stop()
				}
				return
			case syscall.SIGQUIT:
				stacklen := runtime.Stack(buf, true)
				h.log.Infof("=== received SIGQUIT ===\n*** goroutine dump...\n%s\n*** end", buf[:stacklen])
			}
		}
	}
}

// SignalHandlerLoop blocks until it receives a SIGINT, SIGTERM or SIGQUIT.
// For SIGINT and SIGTERM, it exits; for SIGQUIT is print a goroutine stack
// dump.
func SignalHandlerLoop(log log.Interface, ss ...SignalReceiver) {
	NewHandler(log, ss...).Loop()
}
