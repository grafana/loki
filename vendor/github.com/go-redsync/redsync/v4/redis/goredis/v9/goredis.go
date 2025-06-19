package goredis

import (
	"context"
	"strings"
	"time"

	redsyncredis "github.com/go-redsync/redsync/v4/redis"
	"github.com/redis/go-redis/v9"
)

type pool struct {
	delegate redis.UniversalClient
}

func (p *pool) Get(ctx context.Context) (redsyncredis.Conn, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	return &conn{p.delegate, ctx}, nil
}

// NewPool returns a Goredis-based pool implementation.
func NewPool(delegate redis.UniversalClient) redsyncredis.Pool {
	return &pool{delegate}
}

type conn struct {
	delegate redis.UniversalClient
	ctx      context.Context
}

func (c *conn) Get(name string) (string, error) {
	value, err := c.delegate.Get(c.ctx, name).Result()
	return value, noErrNil(err)
}

func (c *conn) Set(name string, value string) (bool, error) {
	reply, err := c.delegate.Set(c.ctx, name, value, 0).Result()
	return reply == "OK", err
}

func (c *conn) SetNX(name string, value string, expiry time.Duration) (bool, error) {
	return c.delegate.SetNX(c.ctx, name, value, expiry).Result()
}

func (c *conn) PTTL(name string) (time.Duration, error) {
	return c.delegate.PTTL(c.ctx, name).Result()
}

func (c *conn) Eval(script *redsyncredis.Script, keysAndArgs ...interface{}) (interface{}, error) {
	keys := make([]string, script.KeyCount)
	args := keysAndArgs

	if script.KeyCount > 0 {
		for i := 0; i < script.KeyCount; i++ {
			keys[i] = keysAndArgs[i].(string)
		}
		args = keysAndArgs[script.KeyCount:]
	}

	v, err := c.delegate.EvalSha(c.ctx, script.Hash, keys, args...).Result()
	if err != nil && strings.Contains(err.Error(), "NOSCRIPT ") {
		v, err = c.delegate.Eval(c.ctx, script.Src, keys, args...).Result()
	}
	return v, noErrNil(err)
}

func (c *conn) Close() error {
	// Not needed for this library
	return nil
}

func noErrNil(err error) error {
	if err == redis.Nil {
		return nil
	}
	return err
}
