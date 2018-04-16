package kuberesolver

import (
	"net"
	"strconv"
	"sync"

	"google.golang.org/grpc/grpclog"
	"google.golang.org/grpc/naming"
)

type watchResult struct {
	ep  *Event
	err error
}

// A Watcher provides name resolution updates from Kubernetes endpoints
// identified by name.
type watcher struct {
	target    targetInfo
	endpoints map[string]interface{}
	stopCh    chan struct{}
	result    chan watchResult
	sync.Mutex
	stopped bool
}

// Close closes the watcher, cleaning up any open connections.
func (w *watcher) Close() {
	close(w.stopCh)
}

// Next updates the endpoints for the name being watched.
func (w *watcher) Next() ([]*naming.Update, error) {
	updates := make([]*naming.Update, 0)
	updatedEndpoints := make(map[string]interface{})
	var ep Event

	select {
	case <-w.stopCh:
		w.Lock()
		if !w.stopped {
			w.stopped = true
		}
		w.Unlock()
		return updates, nil
	case r := <-w.result:
		if r.err == nil {
			ep = *r.ep
		} else {
			return updates, r.err
		}
	}
	for _, subset := range ep.Object.Subsets {
		port := ""
		if w.target.useFirstPort {
			port = strconv.Itoa(subset.Ports[0].Port)
		} else if w.target.resolveByPortName {
			for _, p := range subset.Ports {
				if p.Name == w.target.port {
					port = strconv.Itoa(p.Port)
					break
				}
			}
		} else {
			port = w.target.port
		}

		if len(port) == 0 {
			port = strconv.Itoa(subset.Ports[0].Port)
		}
		for _, address := range subset.Addresses {
			endpoint := net.JoinHostPort(address.IP, port)
			updatedEndpoints[endpoint] = nil
		}
	}

	// Create updates to add new endpoints.
	for addr, md := range updatedEndpoints {
		if _, ok := w.endpoints[addr]; !ok {
			updates = append(updates, &naming.Update{naming.Add, addr, md})
			grpclog.Printf("kuberesolver: %s ADDED to %s", addr, w.target.target)
		}
	}

	// Create updates to delete old endpoints.
	for addr := range w.endpoints {
		if _, ok := updatedEndpoints[addr]; !ok {
			updates = append(updates, &naming.Update{naming.Delete, addr, nil})
			grpclog.Printf("kuberesolver: %s DELETED from %s", addr, w.target.target)
		}
	}
	w.endpoints = updatedEndpoints
	return updates, nil
}
