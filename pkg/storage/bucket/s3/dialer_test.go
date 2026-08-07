package s3

import (
	"context"
	"errors"
	"net"
	"slices"
	"testing"

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
