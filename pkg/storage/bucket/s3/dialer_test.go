package s3

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func ipAddrs(ips ...string) []net.IPAddr {
	addrs := make([]net.IPAddr, 0, len(ips))
	for _, ip := range ips {
		addrs = append(addrs, net.IPAddr{IP: net.ParseIP(ip)})
	}
	return addrs
}

// Check that repeated dials to one hostname do not all land on the same address.
func TestShufflingDialerSpreadsAcrossAddresses(t *testing.T) {
	// Three listeners on distinct loopback addresses, all resolved from one
	// hostname.
	var (
		listeners []net.Listener
		addrs     []net.IPAddr
		port      string // Port is blank the first time so we get a random one.
	)
	for i := range 3 {
		ip := net.IPv4(127, 0, 0, byte(i+1))
		l, err := net.Listen("tcp", net.JoinHostPort(ip.String(), port))
		if err != nil {
			// Only 127.0.0.1 is bindable by default on some platforms.
			t.Skipf("cannot bind %s, loopback aliases unavailable: %v", ip, err)
		}
		t.Cleanup(func() { l.Close() })

		if port == "" {
			// Pull the port out so we use it for subsequent binds.
			_, port, err = net.SplitHostPort(l.Addr().String())
			require.NoError(t, err)
		}

		listeners = append(listeners, l)
		addrs = append(addrs, net.IPAddr{IP: ip})
	}

	d := newShufflingDialer()
	d.lookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return slices.Clone(addrs), nil
	}

	seen := map[string]int{}
	for range 60 {
		conn, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort("s3.example.com", port))
		require.NoError(t, err)
		seen[conn.RemoteAddr().(*net.TCPAddr).IP.String()]++
		conn.Close()
	}

	assert.Len(t, seen, len(listeners), "every address should receive some connections, got %v", seen)
	for _, l := range listeners {
		ip := l.Addr().(*net.TCPAddr).IP.String()
		assert.Positive(t, seen[ip], "address %s received no connections", ip)
	}
}

func TestShufflingDialerFallsBackToNextAddress(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { l.Close() })

	_, port, err := net.SplitHostPort(l.Addr().String())
	require.NoError(t, err)

	// A closed listener gives us an address that reliably refuses connections.
	dead, err := net.Listen("tcp", "127.0.0.2:"+port)
	if err != nil {
		t.Skipf("cannot bind 127.0.0.2, loopback aliases unavailable: %v", err)
	}
	require.NoError(t, dead.Close())

	d := newShufflingDialer()
	d.lookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return ipAddrs("127.0.0.2", "127.0.0.1"), nil
	}
	// Force the unreachable address to be tried first.
	d.shuffle = func([]net.IPAddr) {}

	conn, err := d.DialContext(context.Background(), "tcp", net.JoinHostPort("s3.example.com", port))
	require.NoError(t, err)
	t.Cleanup(func() { conn.Close() })

	assert.Equal(t, "127.0.0.1", conn.RemoteAddr().(*net.TCPAddr).IP.String())
}

func TestOrderAddrsInterleavesFamilies(t *testing.T) {
	d := newShufflingDialer()
	// Keep the order within each family predictable so we can assert on it.
	d.shuffle = func([]net.IPAddr) {}

	for _, tc := range []struct {
		name    string
		network string
		addrs   []net.IPAddr
		want    []net.IPAddr
	}{
		{
			name:    "v4 first stays first",
			network: "tcp",
			addrs:   ipAddrs("192.0.2.1", "192.0.2.2", "2001:db8::1", "2001:db8::2"),
			want:    ipAddrs("192.0.2.1", "2001:db8::1", "192.0.2.2", "2001:db8::2"),
		},
		{
			name:    "v6 first stays first",
			network: "tcp",
			addrs:   ipAddrs("2001:db8::1", "2001:db8::2", "192.0.2.1", "192.0.2.2"),
			want:    ipAddrs("2001:db8::1", "192.0.2.1", "2001:db8::2", "192.0.2.2"),
		},
		{
			name:    "uneven families keep the remainder",
			network: "tcp",
			addrs:   ipAddrs("192.0.2.1", "192.0.2.2", "192.0.2.3", "2001:db8::1"),
			want:    ipAddrs("192.0.2.1", "2001:db8::1", "192.0.2.2", "192.0.2.3"),
		},
		{
			name:    "tcp4 drops v6",
			network: "tcp4",
			addrs:   ipAddrs("2001:db8::1", "192.0.2.1"),
			want:    ipAddrs("192.0.2.1"),
		},
		{
			name:    "tcp6 drops v4",
			network: "tcp6",
			addrs:   ipAddrs("192.0.2.1", "2001:db8::1"),
			want:    ipAddrs("2001:db8::1"),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, d.orderAddrs(tc.network, tc.addrs))
		})
	}
}

func TestShufflingDialerReturnsLookupError(t *testing.T) {
	d := newShufflingDialer()
	d.lookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return nil, errors.New("no such host")
	}

	_, err := d.DialContext(context.Background(), "tcp", "s3.example.com:443")
	assert.ErrorContains(t, err, "no such host")
}

func TestShufflingDialerSkipsResolutionForLiteralIP(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { l.Close() })

	d := newShufflingDialer()
	d.lookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		t.Fatal("literal IP must not be resolved")
		return nil, nil
	}

	conn, err := d.DialContext(context.Background(), "tcp", l.Addr().String())
	require.NoError(t, err)
	require.NoError(t, conn.Close())
}

func TestShufflingDialerHonoursContextCancellation(t *testing.T) {
	d := newShufflingDialer()
	d.lookupIPAddr = func(_ context.Context, _ string) ([]net.IPAddr, error) {
		return ipAddrs("127.0.0.1"), nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := d.DialContext(ctx, "tcp", "s3.example.com:1")
	assert.Error(t, err)
}

// dialerCounter reads a go-conntrack counter for one dialer name off the
// default registry, which is where go-conntrack registers its metrics.
func dialerCounter(t *testing.T, metric, dialerName string) float64 {
	t.Helper()

	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)

	total := 0.0
	for _, family := range families {
		if family.GetName() != metric {
			continue
		}
		for _, m := range family.GetMetric() {
			for _, label := range m.GetLabel() {
				if label.GetName() == "dialer_name" && label.GetValue() == dialerName {
					total += m.GetCounter().GetValue()
				}
			}
		}
	}
	return total
}

func TestInstrumentedDialContextCountsConnections(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { l.Close() })

	// A port nothing is listening on, to exercise the failure path.
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	deadAddr := dead.Addr().String()
	require.NoError(t, dead.Close())

	name := "test"
	dial := instrumentedDialContextFunc((&net.Dialer{}).DialContext, name)

	// Measured as deltas: go-conntrack registers on the default
	// registry, so a repeated run starts from a non-zero baseline.
	base := map[string]float64{}
	for _, metric := range []string{"attempted", "established", "failed", "closed"} {
		metric = "net_conntrack_dialer_conn_" + metric + "_total"
		base[metric] = dialerCounter(t, metric, name)
	}
	counter := func(metric string) float64 {
		metric = "net_conntrack_dialer_conn_" + metric + "_total"
		return dialerCounter(t, metric, name) - base[metric]
	}

	conn, err := dial(context.Background(), "tcp", l.Addr().String())
	require.NoError(t, err)

	assert.Equal(t, 1.0, counter("attempted"))
	assert.Equal(t, 1.0, counter("established"))
	assert.Equal(t, 0.0, counter("closed"))

	require.NoError(t, conn.Close())
	assert.Equal(t, 1.0, counter("closed"))

	_, err = dial(context.Background(), "tcp", deadAddr)
	require.Error(t, err)

	assert.Equal(t, 2.0, counter("attempted"))
	assert.Equal(t, 1.0, counter("established"))
	// Summed across reasons rather than compared exactly: go-conntrack
	// counts a refused connection under both "refused" and "unknown".
	assert.GreaterOrEqual(t, counter("failed"), 1.0)
}
