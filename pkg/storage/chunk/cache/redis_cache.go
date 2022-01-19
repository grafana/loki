package cache

import (
	"context"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"

	util_log "github.com/grafana/loki/pkg/util/log"
)

// RedisCache type caches chunks in redis
type RedisCache struct {
	name   string
	redis  *RedisClient
	logger log.Logger
}

// NewRedisCache creates a new RedisCache
func NewRedisCache(name string, redisClient *RedisClient, logger log.Logger) *RedisCache {
	util_log.WarnExperimentalUse("Redis cache", logger)
	cache := &RedisCache{
		name:   name,
		redis:  redisClient,
		logger: logger,
	}
	if err := cache.redis.Ping(context.Background()); err != nil {
		level.Error(logger).Log("msg", "error connecting to redis", "name", name, "err", err)
	}
	return cache
}

// Fetch gets keys from the cache. The keys that are found must be in the order of the keys requested.
func (c *RedisCache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missed []string, err error) {
	data, err := c.redis.MGet(ctx, keys)
	if err != nil {
		level.Error(c.logger).Log("msg", "failed to get from redis", "name", c.name, "err", err)
		missed = make([]string, len(keys))
		copy(missed, keys)
		return
	}
	for i, key := range keys {
		if data[i] != nil {
			found = append(found, key)
			bufs = append(bufs, data[i])
		} else {
			missed = append(missed, key)
		}
	}
	return
}

// Store stores the key in the cache.
func (c *RedisCache) Store(ctx context.Context, keys []string, bufs [][]byte) error {
	err := c.redis.MSet(ctx, keys, bufs)
	if err != nil {
		level.Error(c.logger).Log("msg", "failed to put to redis", "name", c.name, "err", err)
	}
	return err
}

// Stop stops the redis client.
func (c *RedisCache) Stop() {
	_ = c.redis.Close()
}
