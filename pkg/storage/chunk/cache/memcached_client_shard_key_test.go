package cache

import (
	"errors"
	"net"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/grafana/gomemcache/memcache"
	"github.com/stretchr/testify/require"
)

func TestMemcachedClient_Set_ShardKey(t *testing.T) {
	t.Run("picks the server from the shard key and names it in the error", func(t *testing.T) {
		client, selector := newFailingMemcachedClient(t)

		err := client.Set(&memcache.Item{Key: "item"}, memcache.WithShardKey("shard"))

		require.ErrorContains(t, err, "server="+selector.addr.String())
		require.Equal(t, []string{"shard"}, slices.Compact(selector.pickedKeys()))
	})

	t.Run("picks the server from the item key when the call has no shard key", func(t *testing.T) {
		client, selector := newFailingMemcachedClient(t)

		err := client.Set(&memcache.Item{Key: "item"})

		require.ErrorContains(t, err, "server="+selector.addr.String())
		require.Equal(t, []string{"item"}, slices.Compact(selector.pickedKeys()))
	})
}

// newFailingMemcachedClient returns a client and its selector. The client fails
// to dial its only server, so every call fails at once without a network.
func newFailingMemcachedClient(t *testing.T) (*memcachedClient, *recordingSelector) {
	selector := &recordingSelector{addr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 11211}}
	client := memcache.NewFromSelector(selector)
	client.DialTimeout = func(string, string, time.Duration) (net.Conn, error) {
		return nil, errors.New("dial failed")
	}
	t.Cleanup(client.Close)

	return &memcachedClient{Client: client, serverList: selector}, selector
}

// recordingSelector picks the same server for every key and records the keys.
type recordingSelector struct {
	addr net.Addr

	mtx    sync.Mutex
	picked []string
}

func (s *recordingSelector) PickServer(key string) (net.Addr, error) {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	s.picked = append(s.picked, key)
	return s.addr, nil
}

func (s *recordingSelector) Each(fn func(net.Addr) error) error {
	return fn(s.addr)
}

func (s *recordingSelector) SetServers(...string) error {
	return nil
}

func (s *recordingSelector) pickedKeys() []string {
	s.mtx.Lock()
	defer s.mtx.Unlock()
	return slices.Clone(s.picked)
}
