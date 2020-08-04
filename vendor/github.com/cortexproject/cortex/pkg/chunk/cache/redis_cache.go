package cache

import (
	"context"
	"flag"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/gomodule/redigo/redis"

	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

// RedisCache type caches chunks in redis
type RedisCache struct {
	name       string
	expiration int
	timeout    time.Duration
	pool       *redis.Pool
	logger     log.Logger
}

// RedisConfig defines how a RedisCache should be constructed.
type RedisConfig struct {
	Endpoint             string         `yaml:"endpoint"`
	Timeout              time.Duration  `yaml:"timeout"`
	Expiration           time.Duration  `yaml:"expiration"`
	MaxIdleConns         int            `yaml:"max_idle_conns"`
	MaxActiveConns       int            `yaml:"max_active_conns"`
	Password             flagext.Secret `yaml:"password"`
	EnableTLS            bool           `yaml:"enable_tls"`
	IdleTimeout          time.Duration  `yaml:"idle_timeout"`
	WaitOnPoolExhaustion bool           `yaml:"wait_on_pool_exhaustion"`
	MaxConnLifetime      time.Duration  `yaml:"max_conn_lifetime"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *RedisConfig) RegisterFlagsWithPrefix(prefix, description string, f *flag.FlagSet) {
	f.StringVar(&cfg.Endpoint, prefix+"redis.endpoint", "", description+"Redis service endpoint to use when caching chunks. If empty, no redis will be used.")
	f.DurationVar(&cfg.Timeout, prefix+"redis.timeout", 100*time.Millisecond, description+"Maximum time to wait before giving up on redis requests.")
	f.DurationVar(&cfg.Expiration, prefix+"redis.expiration", 0, description+"How long keys stay in the redis.")
	f.IntVar(&cfg.MaxIdleConns, prefix+"redis.max-idle-conns", 80, description+"Maximum number of idle connections in pool.")
	f.IntVar(&cfg.MaxActiveConns, prefix+"redis.max-active-conns", 0, description+"Maximum number of active connections in pool.")
	f.Var(&cfg.Password, prefix+"redis.password", description+"Password to use when connecting to redis.")
	f.BoolVar(&cfg.EnableTLS, prefix+"redis.enable-tls", false, description+"Enables connecting to redis with TLS.")
	f.DurationVar(&cfg.IdleTimeout, prefix+"redis.idle-timeout", 0, description+"Close connections after remaining idle for this duration. If the value is zero, then idle connections are not closed.")
	f.BoolVar(&cfg.WaitOnPoolExhaustion, prefix+"redis.wait-on-pool-exhaustion", false, description+"Enables waiting if there are no idle connections. If the value is false and the pool is at the max_active_conns limit, the pool will return a connection with ErrPoolExhausted error and not wait for idle connections.")
	f.DurationVar(&cfg.MaxConnLifetime, prefix+"redis.max-conn-lifetime", 0, description+"Close connections older than this duration. If the value is zero, then the pool does not close connections based on age.")
}

// NewRedisCache creates a new RedisCache
func NewRedisCache(cfg RedisConfig, name string, pool *redis.Pool, logger log.Logger) *RedisCache {
	util.WarnExperimentalUse("Redis cache")
	// pool != nil only in unit tests
	if pool == nil {
		pool = &redis.Pool{
			Dial: func() (redis.Conn, error) {
				options := make([]redis.DialOption, 0, 2)
				if cfg.EnableTLS {
					options = append(options, redis.DialUseTLS(true))
				}
				if cfg.Password.Value != "" {
					options = append(options, redis.DialPassword(cfg.Password.Value))
				}

				c, err := redis.Dial("tcp", cfg.Endpoint, options...)
				if err != nil {
					return nil, err
				}
				return c, err
			},
			MaxIdle:         cfg.MaxIdleConns,
			MaxActive:       cfg.MaxActiveConns,
			IdleTimeout:     cfg.IdleTimeout,
			Wait:            cfg.WaitOnPoolExhaustion,
			MaxConnLifetime: cfg.MaxConnLifetime,
		}
	}

	cache := &RedisCache{
		expiration: int(cfg.Expiration.Seconds()),
		timeout:    cfg.Timeout,
		name:       name,
		pool:       pool,
		logger:     logger,
	}

	if err := cache.ping(context.Background()); err != nil {
		level.Error(logger).Log("msg", "error connecting to redis", "endpoint", cfg.Endpoint, "err", err)
	}

	return cache
}

// Fetch gets keys from the cache. The keys that are found must be in the order of the keys requested.
func (c *RedisCache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missed []string) {
	data, err := c.mget(ctx, keys)

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
func (c *RedisCache) Store(ctx context.Context, keys []string, bufs [][]byte) {
	err := c.mset(ctx, keys, bufs, c.expiration)
	if err != nil {
		level.Error(c.logger).Log("msg", "failed to put to redis", "name", c.name, "err", err)
	}
}

// Stop stops the redis client.
func (c *RedisCache) Stop() {
	_ = c.pool.Close()
}

// mset adds key-value pairs to the cache.
func (c *RedisCache) mset(_ context.Context, keys []string, bufs [][]byte, ttl int) error {
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
func (c *RedisCache) mget(_ context.Context, keys []string) ([][]byte, error) {
	intf := make([]interface{}, len(keys))
	for i, key := range keys {
		intf[i] = key
	}

	conn := c.pool.Get()
	defer conn.Close()

	return redis.ByteSlices(redis.DoWithTimeout(conn, c.timeout, "MGET", intf...))
}

func (c *RedisCache) ping(_ context.Context) error {
	conn := c.pool.Get()
	defer conn.Close()

	pong, err := redis.DoWithTimeout(conn, c.timeout, "PING")
	if err == nil {
		_, err = redis.String(pong, err)
	}
	return err
}
