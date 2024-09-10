package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRedisClient(t *testing.T) {
	single, err := mockRedisClientSingle()
	require.Nil(t, err)
	defer single.Close()

	cluster, err := mockRedisClientCluster()
	require.Nil(t, err)
	defer cluster.Close()

	ctx := context.Background()

	tests := []struct {
		name   string
		client *RedisClient
	}{
		{
			name:   "single redis client",
			client: single,
		},
		{
			name:   "cluster redis client",
			client: cluster,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys := []string{"key1", "key2", "key3"}
			bufs := [][]byte{[]byte("data1"), []byte("data2"), []byte("data3")}
			miss := []string{"miss1", "miss2"}

			// set values
			err := tt.client.MSet(ctx, keys, bufs)
			require.Nil(t, err)

			// get keys
			values, err := tt.client.MGet(ctx, keys)
			require.Nil(t, err)
			require.Len(t, values, len(keys))
			for i, value := range values {
				require.Equal(t, values[i], value)
			}

			// get missing keys
			values, err = tt.client.MGet(ctx, miss)
			require.Nil(t, err)
			require.Len(t, values, len(miss))
			for _, value := range values {
				require.Nil(t, value)
			}
		})
	}
}

func mockRedisClientSingle() (*RedisClient, error) {
	redisServer, err := miniredis.Run()
	if err != nil {
		return nil, err
	}
	return &RedisClient{
		expiration: time.Minute,
		timeout:    100 * time.Millisecond,
		rdb: redis.NewClient(&redis.Options{
			Addr: redisServer.Addr(),
		}),
	}, nil
}

func mockRedisClientCluster() (*RedisClient, error) {
	redisServer1, err := miniredis.Run()
	if err != nil {
		return nil, err
	}
	redisServer2, err := miniredis.Run()
	if err != nil {
		return nil, err
	}
	return &RedisClient{
		expiration: time.Minute,
		timeout:    100 * time.Millisecond,
		rdb: redis.NewClusterClient(&redis.ClusterOptions{
			Addrs: []string{
				redisServer1.Addr(),
				redisServer2.Addr(),
			},
		}),
	}, nil
}

func Test_deriveEndpoints(t *testing.T) {
	const (
		upstream   = "upstream"
		downstream = "downstream"
		lookback   = "localhost"
	)

	tests := []struct {
		name      string
		endpoints string
		lookup    func(host string) ([]string, error)
		want      []string
		wantErr   string
	}{
		{
			name:      "single endpoint",
			endpoints: fmt.Sprintf("%s:6379", upstream),
			lookup: func(_ string) ([]string, error) {
				return []string{upstream}, nil
			},
			want:    []string{fmt.Sprintf("%s:6379", upstream)},
			wantErr: "",
		},
		{
			name:      "multiple endpoints",
			endpoints: fmt.Sprintf("%s:6379,%s:6379", upstream, downstream), // note the space
			lookup: func(host string) ([]string, error) {
				return []string{host}, nil
			},
			want:    []string{fmt.Sprintf("%s:6379", upstream), fmt.Sprintf("%s:6379", downstream)},
			wantErr: "",
		},
		{
			name:      "all loopback",
			endpoints: fmt.Sprintf("%s:6379", lookback),
			lookup: func(_ string) ([]string, error) {
				return []string{"::1", "127.0.0.1"}, nil
			},
			want:    []string{fmt.Sprintf("%s:6379", lookback)},
			wantErr: "",
		},
		{
			name:      "non-loopback address resolving to multiple addresses",
			endpoints: fmt.Sprintf("%s:6379", upstream),
			lookup: func(_ string) ([]string, error) {
				return []string{upstream, downstream}, nil
			},
			want:    []string{fmt.Sprintf("%s:6379", upstream), fmt.Sprintf("%s:6379", downstream)},
			wantErr: "",
		},
		{
			name:      "no such host",
			endpoints: fmt.Sprintf("%s:6379", upstream),
			lookup: func(_ string) ([]string, error) {
				return nil, fmt.Errorf("no such host")
			},
			want:    nil,
			wantErr: "no such host",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := deriveEndpoints(tt.endpoints, tt.lookup)
			if tt.wantErr != "" {
				require.ErrorContains(t, err, tt.wantErr)
			} else {
				require.NoError(t, err)
			}
			assert.Equalf(t, tt.want, got, "failed to derive correct endpoints from %v", tt.endpoints)
		})
	}
}
