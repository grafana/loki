package cache

import (
	"context"
	"flag"
	"time"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/gomodule/redigo/redis"
)

// RedisCache type caches chunks in redis
type RedisCache struct {
	name       string
	expiration int
	timeout    time.Duration
	pool       *redis.Pool
}

// RedisConfig defines how a RedisCache should be constructed.
type RedisConfig struct {
	Endpoint       string        `yaml:"endpoint,omitempty"`
	Timeout        time.Duration `yaml:"timeout,omitempty"`
	Expiration     time.Duration `yaml:"expiration,omitempty"`
	MaxIdleConns   int           `yaml:"max_idle_conns,omitempty"`
	MaxActiveConns int           `yaml:"max_active_conns,omitempty"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *RedisConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.StringVar(&cfg.Endpoint, prefix+"redis.endpoint", "", description+"Redis service endpoint to use when caching chunks. If empty, no redis will be used.")
	f.DurationVar(&cfg.Timeout, prefix+"redis.timeout", 100*time.Millisecond, description+"Maximum time to wait before giving up on redis requests.")
	f.DurationVar(&cfg.Expiration, prefix+"redis.expiration", 0, description+"How long keys stay in the redis.")
	f.IntVar(&cfg.MaxIdleConns, prefix+"redis.max-idle-conns", 80, description+"Maximum number of idle connections in pool.")
	f.IntVar(&cfg.MaxActiveConns, prefix+"redis.max-active-conns", 0, description+"Maximum number of active connections in pool.")
}

// NewRedisCache creates a new RedisCache
func NewRedisCache(cfg RedisConfig, name string, pool *redis.Pool) *RedisCache {
	// pool != nil only in unit tests
	if pool == nil {
		pool = &redis.Pool{
			MaxIdle:   cfg.MaxIdleConns,
			MaxActive: cfg.MaxActiveConns,
			Dial: func() (redis.Conn, error) {
				c, err := redis.Dial("tcp", cfg.Endpoint)
				if err != nil {
					return nil, err
				}
				return c, err
			},
		}
	}

	cache := &RedisCache{
		expiration: int(cfg.Expiration.Seconds()),
		timeout:    cfg.Timeout,
		name:       name,
		pool:       pool,
	}

	if err := cache.ping(context.Background()); err != nil {
		level.Error(util.Logger).Log("msg", "error connecting to redis", "endpoint", cfg.Endpoint, "err", err)
	}

	return cache
}

// Fetch gets keys from the cache. The keys that are found must be in the order of the keys requested.
func (c *RedisCache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missed []string) {
	data, err := c.mget(ctx, keys)

	if err != nil {
		level.Error(util.Logger).Log("msg", "failed to get from redis", "name", c.name, "err", err)
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
func (c *RedisCache) Store(ctx context.Context, keys []string, bufs [][]byte) {
	err := c.mset(ctx, keys, bufs, c.expiration)
	if err != nil {
		level.Error(util.Logger).Log("msg", "failed to put to redis", "name", c.name, "err", err)
	}
}

// Stop stops the redis client.
func (c *RedisCache) Stop() error {
	return c.pool.Close()
}

// mset adds key-value pairs to the cache.
func (c *RedisCache) mset(ctx context.Context, keys []string, bufs [][]byte, ttl int) error {
	conn := c.pool.Get()
	defer conn.Close()

	if err := conn.Send("MULTI"); err != nil {
		return err
	}
	for i := range keys {
		if err := conn.Send("SETEX", keys[i], ttl, bufs[i]); err != nil {
			return err
		}
	}
	_, err := redis.DoWithTimeout(conn, c.timeout, "EXEC")
	return err
}

// mget retrieves values from the cache.
func (c *RedisCache) mget(ctx context.Context, keys []string) ([][]byte, error) {
	intf := make([]interface{}, len(keys))
	for i, key := range keys {
		intf[i] = key
	}

	conn := c.pool.Get()
	defer conn.Close()

	return redis.ByteSlices(redis.DoWithTimeout(conn, c.timeout, "MGET", intf...))
}

func (c *RedisCache) ping(ctx context.Context) error {
	conn := c.pool.Get()
	defer conn.Close()

	pong, err := redis.DoWithTimeout(conn, c.timeout, "PING")
	if err == nil {
		_, err = redis.String(pong, err)
	}
	return err
}
