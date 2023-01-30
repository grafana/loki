package services

import (
	"github.com/pkg/errors"
)

var (
	errFailureWatcherNotInitialized = errors.New("FailureWatcher has not been initialized")
)

// FailureWatcher waits for service failures, and passed them to the channel.
type FailureWatcher struct {
	ch chan error
}

func NewFailureWatcher() *FailureWatcher {
	return &FailureWatcher{ch: make(chan error)}
}

// Chan returns channel for this watcher. If watcher is nil, returns nil channel.
// Errors returned on the channel include failure case and service description.
func (w *FailureWatcher) Chan() <-chan error {
	// Graceful handle the case FailureWatcher has not been initialized,
	// to simplify the code in the components using it.
	if w == nil {
		return nil
	}
	return w.ch
}

func (w *FailureWatcher) WatchService(service Service) {
	// Ensure that if the caller request to watch a service, then the FailureWatcher
	// has been initialized.
	if w == nil {
		panic(errFailureWatcherNotInitialized)
	}

	service.AddListener(NewListener(nil, nil, nil, nil, func(from State, failure error) {
		w.ch <- errors.Wrapf(failure, "service %s failed", DescribeService(service))
	}))
}

func (w *FailureWatcher) WatchManager(manager *Manager) {
	// Ensure that if the caller request to watch services, then the FailureWatcher
	// has been initialized.
	if w == nil {
		panic(errFailureWatcherNotInitialized)
	}

	manager.AddListener(NewManagerListener(nil, nil, func(service Service) {
		w.ch <- errors.Wrapf(service.FailureCase(), "service %s failed", DescribeService(service))
	}))
}
