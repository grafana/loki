package jumphash

import (
	"fmt"
	"net"
	"testing"

	"github.com/facette/natsort"
	"github.com/grafana/gomemcache/memcache"
	"github.com/stretchr/testify/require"
)

func TestNatSort(t *testing.T) {
	// Validate that the order of SRV records returned by a DNS
	// lookup for a k8s StatefulSet are ordered as expected when
	// a natsort is done.
	input := []string{
		"memcached-10.memcached.cortex.svc.cluster.local.",
		"memcached-1.memcached.cortex.svc.cluster.local.",
		"memcached-6.memcached.cortex.svc.cluster.local.",
		"memcached-3.memcached.cortex.svc.cluster.local.",
		"memcached-25.memcached.cortex.svc.cluster.local.",
	}

	expected := []string{
		"memcached-1.memcached.cortex.svc.cluster.local.",
		"memcached-3.memcached.cortex.svc.cluster.local.",
		"memcached-6.memcached.cortex.svc.cluster.local.",
		"memcached-10.memcached.cortex.svc.cluster.local.",
		"memcached-25.memcached.cortex.svc.cluster.local.",
	}

	natsort.Sort(input)
	require.Equal(t, expected, input)
}

var mockUnixResolver = func(network, address string) (*net.UnixAddr, error) {
	return &net.UnixAddr{
		Name: address,
		Net:  network,
	}, nil
}

var ips = map[string][]byte{
	"google.com:80":     net.ParseIP("198.51.100.121"),
	"duckduckgo.com:80": net.ParseIP("10.0.0.1"),
	"microsoft.com:80":  net.ParseIP("172.12.34.56"),
}

var mockTCPResolver = func(_, address string) (*net.TCPAddr, error) {
	return &net.TCPAddr{
		IP:   ips[address],
		Port: 0,
		Zone: "",
	}, nil
}

func TestMemcachedJumpHashSelector_PickSever(t *testing.T) {
	s := NewSelector(
		"test",
		mockUnixResolver,
		mockTCPResolver,
	)
	err := s.SetServers("google.com:80", "microsoft.com:80", "duckduckgo.com:80")
	require.NoError(t, err)

	// We store the string representation instead of the net.Addr
	// to make sure different IPs were discovered during SetServers
	distribution := make(map[string]int)

	for i := 0; i < 100; i++ {
		key := fmt.Sprintf("key-%d", i)
		addr, err := s.PickServer(key)
		require.NoError(t, err)
		distribution[addr.String()]++
	}

	// All of the servers should have been returned at least
	// once
	require.Len(t, distribution, 3)
	for _, v := range distribution {
		require.NotZero(t, v)
	}
}

func TestMemcachedJumpHashSelector_PickSever_ErrNoServers(t *testing.T) {
	s := NewSelector(
		"test",
		mockUnixResolver,
		mockTCPResolver,
	)
	_, err := s.PickServer("foo")
	require.Error(t, memcache.ErrNoServers, err)
}
