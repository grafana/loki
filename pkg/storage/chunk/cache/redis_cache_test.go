package cache

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-kit/log"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestRedisCache(t *testing.T) {
	c, err := mockRedisCache()
	require.Nil(t, err)
	defer c.redis.Close()

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

func mockRedisCache() (*RedisCache, error) {
	redisServer, err := miniredis.Run()
	if err != nil {
		return nil, err

	}
	redisClient := &RedisClient{
		expiration: time.Minute,
		timeout:    100 * time.Millisecond,
		rdb: redis.NewUniversalClient(&redis.UniversalOptions{
			Addrs: []string{redisServer.Addr()},
		}),
	}
	return NewRedisCache("mock", redisClient, log.NewNopLogger(), "test"), nil
}
