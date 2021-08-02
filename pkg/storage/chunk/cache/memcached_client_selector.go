package cache

import (
	"net"
	"strings"
	"sync"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/cespare/xxhash"
	"github.com/facette/natsort"
)

// MemcachedJumpHashSelector implements the memcache.ServerSelector
// interface. MemcachedJumpHashSelector utilizes a jump hash to
// distribute keys to servers.
//
// While adding or removing servers only requires 1/N keys to move,
// servers are treated as a stack and can only be pushed/popped.
// Therefore, MemcachedJumpHashSelector works best for servers
// with consistent DNS names where the naturally sorted order
// is predictable.
type MemcachedJumpHashSelector struct {
	mu    sync.RWMutex
	addrs []net.Addr
}

// staticAddr caches the Network() and String() values from
// any net.Addr.
//
// Copied from github.com/bradfitz/gomemcache/selector.go.
type staticAddr struct {
	network, str string
}

func newStaticAddr(a net.Addr) net.Addr {
	return &staticAddr{
		network: a.Network(),
		str:     a.String(),
	}
}

func (a *staticAddr) Network() string { return a.network }
func (a *staticAddr) String() string  { return a.str }

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

	naddrs := make([]net.Addr, len(sortedServers))
	for i, server := range sortedServers {
		if strings.Contains(server, "/") {
			addr, err := net.ResolveUnixAddr("unix", server)
			if err != nil {
				return err
			}
			naddrs[i] = newStaticAddr(addr)
		} else {
			tcpAddr, err := net.ResolveTCPAddr("tcp", server)
			if err != nil {
				return err
			}
			naddrs[i] = newStaticAddr(tcpAddr)
		}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.addrs = naddrs
	return nil
}

// jumpHash consistently chooses a hash bucket number in the range [0, numBuckets) for the given key.
// numBuckets must be >= 1.
//
// Copied from github.com/dgryski/go-jump/blob/master/jump.go
func jumpHash(key uint64, numBuckets int) int32 {

	var b int64 = -1
	var j int64

	for j < int64(numBuckets) {
		b = j
		key = key*2862933555777941757 + 1
		j = int64(float64(b+1) * (float64(int64(1)<<31) / float64((key>>33)+1)))
	}

	return int32(b)
}

// PickServer returns the server address that a given item
// should be shared onto.
func (s *MemcachedJumpHashSelector) PickServer(key string) (net.Addr, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.addrs) == 0 {
		return nil, memcache.ErrNoServers
	} else if len(s.addrs) == 1 {
		return s.addrs[0], nil
	}
	cs := xxhash.Sum64String(key)
	idx := jumpHash(cs, len(s.addrs))
	return s.addrs[idx], nil
}

// Each iterates over each server and calls the given function.
// If f returns a non-nil error, iteration will stop and that
// error will be returned.
func (s *MemcachedJumpHashSelector) Each(f func(net.Addr) error) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, def := range s.addrs {
		if err := f(def); err != nil {
			return err
		}
	}
	return nil
}
