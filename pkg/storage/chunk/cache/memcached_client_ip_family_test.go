package cache

import (
	"context"
	"errors"
	"net"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIPFamilyNetwork(t *testing.T) {
	for _, tc := range []struct {
		family  string
		network string
		wantErr bool
	}{
		{family: "", network: "ip"},
		{family: "ipv4", network: "ip4"},
		{family: "ipv6", network: "ip6"},
		{family: "tcp4", wantErr: true},
	} {
		t.Run(tc.family, func(t *testing.T) {
			network, err := ipFamilyNetwork(tc.family)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.network, network)
		})
	}
}

func TestTCPResolverForFamily(t *testing.T) {
	for _, tc := range []struct {
		family  string
		address string
		wantErr bool
	}{
		{family: "", address: "127.0.0.1:11211"},
		{family: "", address: "[::1]:11211"},
		{family: "ipv4", address: "127.0.0.1:11211"},
		{family: "ipv4", address: "[::1]:11211", wantErr: true},
		{family: "ipv6", address: "[::1]:11211"},
		{family: "ipv6", address: "127.0.0.1:11211", wantErr: true},
	} {
		t.Run(tc.family+" "+tc.address, func(t *testing.T) {
			addr, err := tcpResolverForFamily(tc.family)("tcp", tc.address)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.address, addr.String())
		})
	}
}

func TestMemcachedClientResolveServers(t *testing.T) {
	records := map[string][]net.IP{
		"memcached-0.memcached.svc": {net.ParseIP("10.0.0.1")},
		"memcached-1.memcached.svc": {net.ParseIP("10.0.0.2"), net.ParseIP("10.0.0.3")},
	}

	for _, tc := range []struct {
		name     string
		family   string
		servers  []string
		lookup   func(ctx context.Context, network, host string) ([]net.IP, error)
		expected []string
		wantErr  bool
	}{
		{
			name:     "ipv4 resolves hostnames and keeps the port",
			family:   "ipv4",
			servers:  []string{"memcached-0.memcached.svc:11211", "memcached-1.memcached.svc:11212"},
			expected: []string{"10.0.0.1:11211", "10.0.0.2:11212"},
		},
		{
			name:     "ipv6 address is bracketed",
			family:   "ipv6",
			servers:  []string{"memcached-0.memcached.svc:11211"},
			lookup:   func(_ context.Context, _, _ string) ([]net.IP, error) { return []net.IP{net.ParseIP("fd00::1")}, nil },
			expected: []string{"[fd00::1]:11211"},
		},
		{
			name:     "ip literals and unix sockets are not looked up",
			family:   "ipv4",
			servers:  []string{"10.0.0.9:11211", "[fd00::9]:11211", "/var/run/memcached.sock"},
			expected: []string{"10.0.0.9:11211", "[fd00::9]:11211", "/var/run/memcached.sock"},
		},
		{
			name:    "lookup error is returned",
			family:  "ipv4",
			servers: []string{"unknown.memcached.svc:11211"},
			wantErr: true,
		},
		{
			name:    "no addresses is an error",
			family:  "ipv4",
			servers: []string{"memcached-0.memcached.svc:11211"},
			lookup:  func(_ context.Context, _, _ string) ([]net.IP, error) { return nil, nil },
			wantErr: true,
		},
		{
			name:    "missing port is an error",
			family:  "ipv4",
			servers: []string{"memcached-0.memcached.svc"},
			wantErr: true,
		},
		{
			name:    "unsupported family is an error",
			family:  "both",
			servers: []string{"memcached-0.memcached.svc:11211"},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var networks []string
			lookup := tc.lookup
			if lookup == nil {
				lookup = func(_ context.Context, _, host string) ([]net.IP, error) {
					ips, ok := records[host]
					if !ok {
						return nil, errors.New("no such host")
					}
					return ips, nil
				}
			}

			c := &memcachedClient{
				cfg: MemcachedClientConfig{IPFamily: tc.family},
				lookupIP: func(ctx context.Context, network, host string) ([]net.IP, error) {
					networks = append(networks, network)
					return lookup(ctx, network, host)
				},
			}

			resolved, err := c.resolveServers(context.Background(), tc.servers)
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.expected, resolved)

			want, err := ipFamilyNetwork(tc.family)
			require.NoError(t, err)
			for _, n := range networks {
				require.Equal(t, want, n)
			}
		})
	}
}
