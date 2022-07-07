package cache

import (
	"context"
	"testing"

	"github.com/go-kit/log"
	"github.com/gorilla/mux"
	"github.com/weaveworks/common/server"

	"github.com/grafana/dskit/ring"
	"github.com/stretchr/testify/require"
)

func TestGroupCache(t *testing.T) {
	gc, err := setupGroupCache()
	require.Nil(t, err)

	c := gc.NewGroup("test-group", "test")
	defer c.Stop()

	keys := []string{"key1", "key2", "key3"}
	bufs := [][]byte{[]byte("data1"), []byte("data2"), []byte("data3")}
	miss := []string{"miss1", "miss2"}

	// ensure input correctness
	nHit := len(keys)
	require.Len(t, bufs, nHit)

	nMiss := len(miss)

	ctx := context.Background()

	err = c.Store(ctx, keys, bufs)
	require.NoError(t, err)

	// test hits
	found, data, missed, _ := c.Fetch(ctx, keys)

	require.Len(t, found, nHit)
	require.Len(t, missed, 0)
	for i := 0; i < nHit; i++ {
		require.Equal(t, keys[i], found[i])
		require.Equal(t, bufs[i], data[i])
	}

	// test misses
	found, _, missed, _ = c.Fetch(ctx, miss)

	require.Len(t, found, 0)
	require.Len(t, missed, nMiss)
	for i := 0; i < nMiss; i++ {
		require.Equal(t, miss[i], missed[i])
	}
}

func setupGroupCache() (*GroupCache, error) {
	return NewGroupCache(
		&mockRingManager{},
		&server.Server{HTTP: mux.NewRouter()},
		log.NewNopLogger(),
		nil,
	)
}

type mockRingManager struct{}

func (rm *mockRingManager) Addr() string {
	return "http://localhost:1234"
}

func (rm *mockRingManager) Ring() ring.ReadRing {
	return &mockRing{}
}

type mockRing struct {
	ring.ReadRing
}

func (r *mockRing) GetAllHealthy(op ring.Operation) (ring.ReplicationSet, error) {
	return ring.ReplicationSet{Instances: []ring.InstanceDesc{
		{
			Addr: "http://localhost:1234",
		},
	}}, nil
}
