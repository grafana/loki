package etcd

import (
	"context"
	"flag"
	"fmt"
	"time"

	"github.com/go-kit/kit/log/level"
	"go.etcd.io/etcd/clientv3"

	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/cortexproject/cortex/pkg/util/flagext"
)

// Config for a new etcd.Client.
type Config struct {
	Endpoints   []string      `yaml:"endpoints"`
	DialTimeout time.Duration `yaml:"dial_timeout"`
	MaxRetries  int           `yaml:"max_retries"`
}

// Client implements ring.KVClient for etcd.
type Client struct {
	cfg   Config
	codec codec.Codec
	cli   *clientv3.Client
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet.
func (cfg *Config) RegisterFlagsWithPrefix(f *flag.FlagSet, prefix string) {
	cfg.Endpoints = []string{}
	f.Var((*flagext.Strings)(&cfg.Endpoints), prefix+"etcd.endpoints", "The etcd endpoints to connect to.")
	f.DurationVar(&cfg.DialTimeout, prefix+"etcd.dial-timeout", 10*time.Second, "The dial timeout for the etcd connection.")
	f.IntVar(&cfg.MaxRetries, prefix+"etcd.max-retries", 10, "The maximum number of retries to do for failed ops.")
}

// New makes a new Client.
func New(cfg Config, codec codec.Codec) (*Client, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   cfg.Endpoints,
		DialTimeout: cfg.DialTimeout,
	})
	if err != nil {
		return nil, err
	}

	return &Client{
		cfg:   cfg,
		codec: codec,
		cli:   cli,
	}, nil
}

// CAS implements kv.Client.
func (c *Client) CAS(ctx context.Context, key string, f func(in interface{}) (out interface{}, retry bool, err error)) error {
	var revision int64
	var lastErr error

	for i := 0; i < c.cfg.MaxRetries; i++ {
		resp, err := c.cli.Get(ctx, key)
		if err != nil {
			level.Error(util.Logger).Log("msg", "error getting key", "key", key, "err", err)
			lastErr = err
			continue
		}

		var intermediate interface{}
		if len(resp.Kvs) > 0 {
			intermediate, err = c.codec.Decode(resp.Kvs[0].Value)
			if err != nil {
				level.Error(util.Logger).Log("msg", "error decoding key", "key", key, "err", err)
				lastErr = err
				continue
			}
			revision = resp.Kvs[0].Version
		}

		var retry bool
		intermediate, retry, err = f(intermediate)
		if err != nil {
			if !retry {
				return err
			}
			lastErr = err
			continue
		}

		// Callback returning nil means it doesn't want to CAS anymore.
		if intermediate == nil {
			return nil
		}

		buf, err := c.codec.Encode(intermediate)
		if err != nil {
			level.Error(util.Logger).Log("msg", "error serialising value", "key", key, "err", err)
			lastErr = err
			continue
		}

		result, err := c.cli.Txn(ctx).
			If(clientv3.Compare(clientv3.Version(key), "=", revision)).
			Then(clientv3.OpPut(key, string(buf))).
			Commit()
		if err != nil {
			level.Error(util.Logger).Log("msg", "error CASing", "key", key, "err", err)
			lastErr = err
			continue
		}
		// result is not Succeeded if the the comparison was false, meaning if the modify indexes did not match.
		if !result.Succeeded {
			level.Debug(util.Logger).Log("msg", "failed to CAS, revision and version did not match in etcd", "key", key, "revision", revision)
			continue
		}

		return nil
	}

	if lastErr != nil {
		return lastErr
	}
	return fmt.Errorf("failed to CAS %s", key)
}

// WatchKey implements kv.Client.
func (c *Client) WatchKey(ctx context.Context, key string, f func(interface{}) bool) {
	backoff := util.NewBackoff(ctx, util.BackoffConfig{
		MinBackoff: 1 * time.Second,
		MaxBackoff: 1 * time.Minute,
	})
	for backoff.Ongoing() {
		watchChan := c.cli.Watch(ctx, key)
		for {
			resp, ok := <-watchChan
			if !ok {
				break
			}
			backoff.Reset()

			for _, event := range resp.Events {
				out, err := c.codec.Decode(event.Kv.Value)
				if err != nil {
					level.Error(util.Logger).Log("msg", "error decoding key", "key", key, "err", err)
					continue
				}

				if !f(out) {
					return
				}
			}
		}
	}
}

// WatchPrefix implements kv.Client.
func (c *Client) WatchPrefix(ctx context.Context, key string, f func(string, interface{}) bool) {
	backoff := util.NewBackoff(ctx, util.BackoffConfig{
		MinBackoff: 1 * time.Second,
		MaxBackoff: 1 * time.Minute,
	})
	for backoff.Ongoing() {
		watchChan := c.cli.Watch(ctx, key, clientv3.WithPrefix())
		for {
			resp, ok := <-watchChan
			if !ok {
				break
			}
			backoff.Reset()

			for _, event := range resp.Events {
				out, err := c.codec.Decode(event.Kv.Value)
				if err != nil {
					level.Error(util.Logger).Log("msg", "error decoding key", "key", key, "err", err)
					continue
				}

				if !f(string(event.Kv.Key), out) {
					return
				}
			}
		}
	}
}

// Get implements kv.Client.
func (c *Client) Get(ctx context.Context, key string) (interface{}, error) {
	resp, err := c.cli.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	if len(resp.Kvs) != 1 {
		return nil, fmt.Errorf("got %d kvs, expected 1", len(resp.Kvs))
	}
	return c.codec.Decode(resp.Kvs[0].Value)
}

// Stop does nothing in etcd client.
func (c *Client) Stop() {
	// nothing to do
}
