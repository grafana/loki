package kuberesolver

import (
	"runtime/debug"
	"time"

	"google.golang.org/grpc/grpclog"
)

func until(f func(), period time.Duration, stopCh <-chan struct{}) {
	select {
	case <-stopCh:
		return
	default:
	}
	for {
		func() {
			defer handleCrash()
			f()
		}()
		select {
		case <-stopCh:
			return
		case <-time.After(period):
		}
	}
}

// HandleCrash simply catches a crash and logs an error. Meant to be called via defer.
func handleCrash() {
	if r := recover(); r != nil {
		callers := string(debug.Stack())
		grpclog.Errorf("kuberesolver: recovered from panic: %#v (%v)\n%v", r, r, callers)
	}
}
