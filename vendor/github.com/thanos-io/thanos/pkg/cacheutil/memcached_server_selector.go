// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package cacheutil

import (
	"net"
	"sync"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/cespare/xxhash"
	"github.com/facette/natsort"
)

var (
	addrsPool = sync.Pool{
		New: func() interface{} {
			addrs := make([]net.Addr, 0, 64)
			return &addrs
		},
	}
)

// MemcachedJumpHashSelector implements the memcache.ServerSelector
// interface, utilizing a jump hash to distribute keys to servers.
//
// While adding or removing servers only requires 1/N keys to move,
// servers are treated as a stack and can only be pushed/popped.
// Therefore, MemcachedJumpHashSelector works best for servers
// with consistent DNS names where the naturally sorted order
// is predictable (ie. Kubernetes statefulsets).
type MemcachedJumpHashSelector struct {
	// To avoid copy and pasting all memcache server list logic,
	// we embed it and implement our features on top of it.
	servers memcache.ServerList
}

// SetServers changes a MemcachedJumpHashSelector's set of servers at
// runtime and is safe for concurrent use by multiple goroutines.
//
// Each server is given equal weight. A server is given more weight
// if it's listed multiple times.
//
// SetServers returns an error if any of the server names fail to
// resolve. No attempt is made to connect to the server. If any
// error occurs, no changes are made to the internal server list.
//
// To minimize the number of rehashes for keys when scaling the
// number of servers in subsequent calls to SetServers, servers
// are stored in natural sort order.
func (s *MemcachedJumpHashSelector) SetServers(servers ...string) error {
	sortedServers := make([]string, len(servers))
	copy(sortedServers, servers)
	natsort.Sort(sortedServers)

	return s.servers.SetServers(sortedServers...)
}

// PickServer returns the server address that a given item
// should be shared onto.
func (s *MemcachedJumpHashSelector) PickServer(key string) (net.Addr, error) {
	// Unfortunately we can't read the list of server addresses from
	// the original implementation, so we use Each() to fetch all of them.
	addrs := *(addrsPool.Get().(*[]net.Addr))
	err := s.servers.Each(func(addr net.Addr) error {
		addrs = append(addrs, addr)
		return nil
	})
	if err != nil {
		return nil, err
	}

	// No need of a jump hash in case of 0 or 1 servers.
	if len(addrs) == 0 {
		addrs = (addrs)[:0]
		addrsPool.Put(&addrs)
		return nil, memcache.ErrNoServers
	}
	if len(addrs) == 1 {
		picked := addrs[0]

		addrs = (addrs)[:0]
		addrsPool.Put(&addrs)

		return picked, nil
	}

	// Pick a server using the jump hash.
	cs := xxhash.Sum64String(key)
	idx := jumpHash(cs, len(addrs))
	picked := (addrs)[idx]

	addrs = (addrs)[:0]
	addrsPool.Put(&addrs)

	return picked, nil
}

// Each iterates over each server and calls the given function.
// If f returns a non-nil error, iteration will stop and that
// error will be returned.
func (s *MemcachedJumpHashSelector) Each(f func(net.Addr) error) error {
	return s.servers.Each(f)
}
