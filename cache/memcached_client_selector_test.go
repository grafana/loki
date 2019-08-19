package cache_test

import (
	"fmt"
	"testing"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/facette/natsort"
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

func TestMemcachedJumpHashSelector_PickSever(t *testing.T) {
	s := cache.MemcachedJumpHashSelector{}
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
	s := cache.MemcachedJumpHashSelector{}
	_, err := s.PickServer("foo")
	require.Error(t, memcache.ErrNoServers, err)
}
