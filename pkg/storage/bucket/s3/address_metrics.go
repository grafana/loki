package s3

import (
	"context"
	"net"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/util/constants"
)

// maxTrackedAddresses bounds the memory held by the set of addresses ever seen.
// A regional S3 endpoint exposes far fewer than this, so in practice the cap is
// never reached.
const maxTrackedAddresses = 100_000

var (
	dialerDistinctAddresses = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: constants.Loki,
		Subsystem: "s3",
		Name:      "dialer_distinct_addresses_total",
		Help:      "Number of distinct remote IP addresses connected to since startup. Stops rising once 100000 have been seen.",
	}, []string{"dialer_name"})

	dialerOpenAddresses = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: constants.Loki,
		Subsystem: "s3",
		Name:      "dialer_open_addresses",
		Help:      "Number of distinct remote IP addresses currently holding at least one open connection.",
	}, []string{"dialer_name"})
)

func init() {
	prometheus.MustRegister(dialerDistinctAddresses, dialerOpenAddresses)
}

// addressTracker records which remote IP addresses a dialer connects to.
//
// The two metrics answer different questions: the counter says how many
// addresses we have ever reached, so its increase over a range shows whether
// re-resolving DNS is finding new ones; the gauge says how many we are spread
// across right now, which is what the connection rebalancing is trying to move.
type addressTracker struct {
	mu   sync.Mutex
	seen map[string]struct{} // every address connected to, bounded
	open map[string]int      // address -> currently open connections

	distinct prometheus.Counter
	openGaug prometheus.Gauge
}

func newAddressTracker(dialerName string) *addressTracker {
	return &addressTracker{
		seen:     map[string]struct{}{},
		open:     map[string]int{},
		distinct: dialerDistinctAddresses.WithLabelValues(dialerName),
		openGaug: dialerOpenAddresses.WithLabelValues(dialerName),
	}
}

// wrap returns dial with address tracking added. It must sit below any dialer
// that walks several addresses per call, so that it sees the address actually
// connected to rather than the hostname it started from.
func (t *addressTracker) wrap(dial dialContextFunc) func(ctx context.Context, network, address string) (net.Conn, error) {
	return func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := dial(ctx, network, address)
		if err != nil {
			return nil, err
		}

		addr := remoteIP(conn)
		t.acquire(addr)
		return &trackedConn{Conn: conn, tracker: t, addr: addr}, nil
	}
}

func (t *addressTracker) acquire(addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if _, ok := t.seen[addr]; !ok && len(t.seen) < maxTrackedAddresses {
		t.seen[addr] = struct{}{}
		t.distinct.Inc()
	}

	t.open[addr]++
	if t.open[addr] == 1 {
		t.openGaug.Inc()
	}
}

func (t *addressTracker) release(addr string) {
	t.mu.Lock()
	defer t.mu.Unlock()

	open, ok := t.open[addr]
	if !ok {
		return
	}
	if open > 1 {
		t.open[addr] = open - 1
		return
	}
	delete(t.open, addr)
	t.openGaug.Dec()
}

// remoteIP is the address without its port, so that the metrics count hosts
// rather than connections.
func remoteIP(conn net.Conn) string {
	remote := conn.RemoteAddr()
	if remote == nil {
		return "unknown"
	}
	if host, _, err := net.SplitHostPort(remote.String()); err == nil {
		return host
	}
	return remote.String()
}

// trackedConn releases its address once closed. Close is documented as being
// safe to call more than once, so the release is guarded.
type trackedConn struct {
	net.Conn
	tracker *addressTracker
	addr    string
	once    sync.Once
}

func (c *trackedConn) Close() error {
	c.once.Do(func() { c.tracker.release(c.addr) })
	return c.Conn.Close()
}
