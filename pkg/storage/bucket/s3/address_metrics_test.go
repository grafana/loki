package s3

import (
	"context"
	"net"
	"slices"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// listenLoopback binds one listener per 127.0.0.x address, all on the same
// port, so that dialling them looks like one hostname resolving to several
// addresses.
func listenLoopback(t *testing.T, n int) []string {
	t.Helper()

	var (
		addrs []string
		port  string // Blank the first time so we get a random one.
	)
	for i := range n {
		ip := net.IPv4(127, 0, 0, byte(i+1))
		l, err := net.Listen("tcp", net.JoinHostPort(ip.String(), port))
		if err != nil {
			// Only 127.0.0.1 is bindable by default on some platforms.
			t.Skipf("cannot bind %s, loopback aliases unavailable: %v", ip, err)
		}
		t.Cleanup(func() { l.Close() })

		if port == "" {
			_, port, err = net.SplitHostPort(l.Addr().String())
			require.NoError(t, err)
		}
		addrs = append(addrs, l.Addr().String())
	}
	return addrs
}

func TestAddressTrackerCountsDistinctAddresses(t *testing.T) {
	addrs := listenLoopback(t, 3)

	name := t.Name()
	tracker := newAddressTracker(name)
	dial := tracker.wrap((&net.Dialer{}).DialContext)

	// Measured as deltas: the metrics are global, so a repeated run of this
	// test starts from whatever the previous one left behind.
	baseDistinct := testutil.ToFloat64(dialerDistinctAddresses.WithLabelValues(name))
	baseOpen := testutil.ToFloat64(dialerOpenAddresses.WithLabelValues(name))
	distinct := func() float64 {
		return testutil.ToFloat64(dialerDistinctAddresses.WithLabelValues(name)) - baseDistinct
	}
	open := func() float64 {
		return testutil.ToFloat64(dialerOpenAddresses.WithLabelValues(name)) - baseOpen
	}

	// Two connections to each address: three distinct, three open.
	var conns []net.Conn
	for range 2 {
		for _, addr := range addrs {
			conn, err := dial(context.Background(), "tcp", addr)
			require.NoError(t, err)
			conns = append(conns, conn)
		}
	}
	assert.Equal(t, 3.0, distinct())
	assert.Equal(t, 3.0, open())

	// Closing one of the two connections to an address keeps it open.
	require.NoError(t, conns[0].Close())
	assert.Equal(t, 3.0, open())

	// Closing twice must not decrement twice.
	require.Error(t, conns[0].Close())
	assert.Equal(t, 3.0, open())

	for _, conn := range conns[1:] {
		require.NoError(t, conn.Close())
	}
	assert.Equal(t, 0.0, open())

	// Reconnecting to an address already seen does not raise the distinct count.
	conn, err := dial(context.Background(), "tcp", addrs[0])
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	assert.Equal(t, 3.0, distinct())
	assert.Equal(t, 1.0, open())
}

// The tracker must see the address the shuffling dialer picked, not the
// hostname the transport asked for.
func TestAddressTrackerSeesShuffledAddresses(t *testing.T) {
	listeners := listenLoopback(t, 3)
	_, port, err := net.SplitHostPort(listeners[0])
	require.NoError(t, err)

	resolved := make([]net.IPAddr, 0, len(listeners))
	for _, addr := range listeners {
		host, _, err := net.SplitHostPort(addr)
		require.NoError(t, err)
		resolved = append(resolved, net.IPAddr{IP: net.ParseIP(host)})
	}

	// Same layering as instrumentedDialContext: tracking underneath shuffling.
	name := t.Name()
	baseDistinct := testutil.ToFloat64(dialerDistinctAddresses.WithLabelValues(name))

	tracker := newAddressTracker(name)
	d := newShufflingDialer()
	d.dialContext = tracker.wrap((&net.Dialer{}).DialContext)
	d.lookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return slices.Clone(resolved), nil
	}

	for range 30 {
		conn, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort("s3.example.com", port))
		require.NoError(t, err)
		require.NoError(t, conn.Close())
	}

	// One hostname, but the tracker recorded the addresses behind it.
	assert.Equal(t, 3.0, testutil.ToFloat64(dialerDistinctAddresses.WithLabelValues(name))-baseDistinct)
	assert.Equal(t, 0.0, testutil.ToFloat64(dialerOpenAddresses.WithLabelValues(name)))
}
