package loki

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"
)

// heldRegisterer buffers Prometheus registrations until release.
//
// dskit memberlist.NewKV registers gauge funcs that call KV.State() and only
// afterwards assigns KV.NamedService. State() on that nil interface panics.
// Loki is already serving /metrics when a ring client first builds the KV
// (the logline index builder does this from its partition-ring startup, about
// 9s after the process starts), so a scrape in that window is caught by the
// HTTP recovery handler and pages LokiRequestPanics.
//
// Buffering the registerer passed to memberlist.NewKVInitService keeps those
// collectors off the scrape registry until GetMemberlistKV has returned. By
// then NewKV has set the service, and the registry lock taken when the
// collectors are finally registered happens after that write.
type heldRegisterer struct {
	inner prometheus.Registerer

	mu      sync.Mutex
	held    bool
	pending []prometheus.Collector
}

func newHeldRegisterer(inner prometheus.Registerer) *heldRegisterer {
	return &heldRegisterer{
		inner: inner,
		held:  true,
	}
}

// release publishes every buffered collector onto the inner registerer.
// Later registrations go straight through. Concurrent callers, including the
// sync.Once waiters inside GetMemberlistKV, are safe.
func (h *heldRegisterer) release() {
	h.mu.Lock()
	if !h.held {
		h.mu.Unlock()
		return
	}
	pending := h.pending
	h.pending = nil
	h.held = false
	h.mu.Unlock()

	for _, c := range pending {
		// Match promauto.MustRegister: a bad descriptor should fail startup
		// rather than drop memberlist metrics.
		if err := h.inner.Register(c); err != nil {
			panic(err)
		}
	}
}

func (h *heldRegisterer) Register(c prometheus.Collector) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.held {
		h.pending = append(h.pending, c)
		return nil
	}
	return h.inner.Register(c)
}

func (h *heldRegisterer) MustRegister(cs ...prometheus.Collector) {
	for _, c := range cs {
		if err := h.Register(c); err != nil {
			panic(err)
		}
	}
}

func (h *heldRegisterer) Unregister(c prometheus.Collector) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.held {
		for i, pending := range h.pending {
			if pending == c {
				h.pending = append(h.pending[:i], h.pending[i+1:]...)
				return true
			}
		}
		return false
	}
	return h.inner.Unregister(c)
}
