package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/gomodule/redigo/redis"
	"github.com/rafaeljusto/redigomock"
	"github.com/stretchr/testify/require"
)

func TestRedisCache(t *testing.T) {
	cfg := cache.RedisConfig{
		Timeout: 10 * time.Millisecond,
	}

	conn := redigomock.NewConn()
	conn.Clear()
	pool := redis.NewPool(func() (redis.Conn, error) {
		return conn, nil
	}, 10)

	keys := []string{"key1", "key2", "key3"}
	bufs := [][]byte{[]byte("data1"), []byte("data2"), []byte("data3")}
	miss := []string{"miss1", "miss2"}

	// ensure input correctness
	nHit := len(keys)
	require.Len(t, bufs, nHit)

	// mock Redis Store
	mockRedisStore(conn, keys, bufs)

	//mock cache hit
	keyIntf := make([]interface{}, nHit)
	bufIntf := make([]interface{}, nHit)

	for i := 0; i < nHit; i++ {
		keyIntf[i] = keys[i]
		bufIntf[i] = bufs[i]
	}
	conn.Command("MGET", keyIntf...).Expect(bufIntf)

	// mock cache miss
	nMiss := len(miss)
	missIntf := make([]interface{}, nMiss)
	for i, s := range miss {
		missIntf[i] = s
	}
	conn.Command("MGET", missIntf...).ExpectError(nil)

	// mock the cache
	c := cache.NewRedisCache(cfg, "mock", pool)
	ctx := context.Background()

	c.Store(ctx, keys, bufs)

	// test hits
	found, data, missed := c.Fetch(ctx, keys)

	require.Len(t, found, nHit)
	require.Len(t, missed, 0)
	for i := 0; i < nHit; i++ {
		require.Equal(t, keys[i], found[i])
		require.Equal(t, bufs[i], data[i])
	}

	// test misses
	found, _, missed = c.Fetch(ctx, miss)

	require.Len(t, found, 0)
	require.Len(t, missed, nMiss)
	for i := 0; i < nMiss; i++ {
		require.Equal(t, miss[i], missed[i])
	}
}

func mockRedisStore(conn *redigomock.Conn, keys []string, bufs [][]byte) {
	conn.Command("MULTI")
	ret := []interface{}{}
	for i := range keys {
		conn.Command("SETEX", keys[i], 0, bufs[i])
		ret = append(ret, "OK")
	}
	conn.Command("EXEC").Expect(ret)
}
