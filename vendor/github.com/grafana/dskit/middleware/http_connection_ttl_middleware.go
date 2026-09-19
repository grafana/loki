package middleware

import (
	"fmt"
	"hash/fnv"
	"net/http"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const (
	connectionHeaderKey                     = "Connection"
	connectionHeaderCloseValue              = "close"
	totalClosedConnectionsReasonLabel       = "reason"
	totalClosedConnectionsReasonLimit       = "limit"
	totalClosedConnectionsReasonIdleTimeout = "idle timeout"
)

var (
	errIdleConnectionCheckFrequencyMustBePositive = fmt.Errorf("idle connection check frequency must be positive")
	errMinLessOrEqualThanMax                      = fmt.Errorf("minimum TTL of TCP connections must be less or equal to the maximum TTL of TCP connections")
)

type connectionState struct {
	ttl         time.Duration
	created     time.Time
	lastSeen    time.Time
	idleExpired bool
}

func (c *connectionState) isExpired() bool {
	return time.Since(c.created) > c.ttl
}

func (c *connectionState) isIdleExpired(timeout time.Duration) bool {
	return time.Since(c.lastSeen) > timeout
}

// HTTPConnectionTTLMiddleware is an HTTP middleware that limits the maximum lifetime
// of TCP connections.
type HTTPConnectionTTLMiddleware interface {
	Interface

	// Stop stops the middleware and releases associated resources.
	// It is safe to call Stop even if the middleware was created with TTL disabled.
	Stop()
}

type httpConnectionTTLMiddleware struct {
	minTTL time.Duration
	maxTTL time.Duration

	connectionsMu sync.Mutex
	connections   map[string]*connectionState

	totalOpenConnections   prometheus.Counter
	totalClosedConnections *prometheus.CounterVec

	ticker *time.Ticker
	done   chan struct{}
}

// NewHTTPConnectionTTLMiddleware returns an HTTP middleware that limits the maximum lifetime, TTL,
// of TCP connections. Once the limit is reached, the middleware sends the 'Connection: close'
// response header to the client, as a signal to close the connection.
// For each connection, the TTL is between the given minTTL and maxTTL. If minTTL and maxTTL are <= 0,
// no TTL is assumed.
func NewHTTPConnectionTTLMiddleware(minTTL, maxTTL, idleConnectionCheckFrequency time.Duration, reg prometheus.Registerer) (HTTPConnectionTTLMiddleware, error) {
	if minTTL < 0 {
		minTTL = 0
	}

	if maxTTL < 0 {
		maxTTL = 0
	}

	if minTTL > maxTTL {
		return nil, errMinLessOrEqualThanMax
	}

	rpcLimiter := &httpConnectionTTLMiddleware{
		minTTL:        minTTL,
		maxTTL:        maxTTL,
		connectionsMu: sync.Mutex{},
		connections:   make(map[string]*connectionState),
		totalOpenConnections: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Name: "open_connections_with_ttl_total",
			Help: "Number of connections that connection with TTL middleware started tracking",
		}),
		totalClosedConnections: promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
			Name: "closed_connections_with_ttl_total",
			Help: "Number of connections that connection with TTL middleware closed or stopped tracking",
		}, []string{totalClosedConnectionsReasonLabel}),
	}

	if maxTTL > 0 {
		if idleConnectionCheckFrequency <= 0 {
			return nil, errIdleConnectionCheckFrequencyMustBePositive
		}
		rpcLimiter.ticker = time.NewTicker(idleConnectionCheckFrequency)
		rpcLimiter.done = make(chan struct{})
		go func() {
			defer rpcLimiter.ticker.Stop()
			for {
				select {
				case <-rpcLimiter.ticker.C:
					rpcLimiter.removeIdleExpiredConnections()
				case <-rpcLimiter.done:
					return
				}
			}
		}()
	}

	return rpcLimiter, nil
}

// removeIdleExpiredConnections uses two-phase cleanup for cached connections that have
// been idle (no requests seen) for longer than maxTTL.
// On the first pass, idle entries are marked (idleExpired=true), preserving them so that
// removeExpiredConnection can still find the entry and detect TTL expiry on the next request.
// On the second pass (if no request arrived in between), the entry is deleted.
func (m *httpConnectionTTLMiddleware) removeIdleExpiredConnections() {
	count := 0
	m.connectionsMu.Lock()
	for conn, state := range m.connections {
		if state.isIdleExpired(m.maxTTL) {
			if state.idleExpired {
				delete(m.connections, conn)
				count++
			} else {
				state.idleExpired = true
			}
		}
	}
	m.connectionsMu.Unlock()
	m.totalClosedConnections.WithLabelValues(totalClosedConnectionsReasonIdleTimeout).Add(float64(count))
}

// removeExpiredConnection checks if the given connection expired, and in that case removes it from the cache.
// If the connection is not yet tracked, it creates a new entry. If it exists and is not expired, it updates lastSeen
// and resets the idleExpired flag (preventing a subsequent idle cleanup pass from deleting the entry).
// Returns a boolean indicating if the given connection has been removed.
func (m *httpConnectionTTLMiddleware) removeExpiredConnection(conn string) bool {
	now := time.Now()
	m.connectionsMu.Lock()
	state, exists := m.connections[conn]
	if !exists {
		state = &connectionState{
			ttl:      m.calculateTTL(conn),
			created:  now,
			lastSeen: now,
		}
		m.connections[conn] = state
		m.connectionsMu.Unlock()
		m.totalOpenConnections.Inc()
		return false
	}
	if !state.isExpired() {
		state.lastSeen = now
		state.idleExpired = false
		m.connectionsMu.Unlock()
		return false
	}
	delete(m.connections, conn)
	m.connectionsMu.Unlock()
	m.totalClosedConnections.WithLabelValues(totalClosedConnectionsReasonLimit).Inc()
	return true
}

// calculateTTL calculates the TTL for the given connection. This value is from the range between minTTL and maxTTL,
// and depends on the connection's hash.
func (m *httpConnectionTTLMiddleware) calculateTTL(conn string) time.Duration {
	h := fnv.New64()
	h.Write([]byte(conn))
	hash := h.Sum64()
	minMs := uint64(m.minTTL.Milliseconds())
	maxMs := uint64(m.maxTTL.Milliseconds())
	ttlInMs := minMs + (hash % (maxMs + 1 - minMs))
	return time.Duration(ttlInMs) * time.Millisecond
}

// Stop stops the background ticker goroutine and releases associated resources.
// It is safe to call Stop even if the middleware was created with TTL disabled.
func (m *httpConnectionTTLMiddleware) Stop() {
	if m.done != nil {
		select {
		case <-m.done:
			// Already closed
		default:
			close(m.done)
		}
	}
}

// Wrap implements middleware.Interface, and returns a http.Handler that first
// calculates the TTL of the connection associated with a given http.Request,
// and then checks connection's current lifetime against the calculated TTL.
// If the former exceeds the latter, the resulting http.Handler marks the
// connection as closed in the response header.
func (m *httpConnectionTTLMiddleware) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if m.maxTTL <= 0 {
			next.ServeHTTP(w, r)
			return
		}

		conn := r.RemoteAddr
		if m.removeExpiredConnection(conn) {
			w.Header().Set(connectionHeaderKey, connectionHeaderCloseValue)
		}
		next.ServeHTTP(w, r)
	})
}
