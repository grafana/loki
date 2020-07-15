package consul

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/kit/log/level"
	consul "github.com/hashicorp/consul/api"
	"github.com/hashicorp/go-cleanhttp"
	"github.com/weaveworks/common/instrument"
	"golang.org/x/time/rate"

	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
	"github.com/cortexproject/cortex/pkg/util"
)

const (
	longPollDuration = 10 * time.Second
)

var (
	writeOptions = &consul.WriteOptions{}

	// ErrNotFound is returned by ConsulClient.Get.
	ErrNotFound = fmt.Errorf("Not found")

	backoffConfig = util.BackoffConfig{
		MinBackoff: 1 * time.Second,
		MaxBackoff: 1 * time.Minute,
	}
)

// Config to create a ConsulClient
type Config struct {
	Host              string        `yaml:"host"`
	ACLToken          string        `yaml:"acl_token"`
	HTTPClientTimeout time.Duration `yaml:"http_client_timeout"`
	ConsistentReads   bool          `yaml:"consistent_reads"`
	WatchKeyRateLimit float64       `yaml:"watch_rate_limit"` // Zero disables rate limit
	WatchKeyBurstSize int           `yaml:"watch_burst_size"` // Burst when doing rate-limit, defaults to 1
}

type kv interface {
	CAS(p *consul.KVPair, q *consul.WriteOptions) (bool, *consul.WriteMeta, error)
	Get(key string, q *consul.QueryOptions) (*consul.KVPair, *consul.QueryMeta, error)
	List(path string, q *consul.QueryOptions) (consul.KVPairs, *consul.QueryMeta, error)
	Delete(key string, q *consul.WriteOptions) (*consul.WriteMeta, error)
	Put(p *consul.KVPair, q *consul.WriteOptions) (*consul.WriteMeta, error)
}

// Client is a KV.Client for Consul.
type Client struct {
	kv
	codec codec.Codec
	cfg   Config
}

// RegisterFlags adds the flags required to config this to the given FlagSet
// If prefix is not an empty string it should end with a period.
func (cfg *Config) RegisterFlags(f *flag.FlagSet, prefix string) {
	f.StringVar(&cfg.Host, prefix+"consul.hostname", "localhost:8500", "Hostname and port of Consul.")
	f.StringVar(&cfg.ACLToken, prefix+"consul.acl-token", "", "ACL Token used to interact with Consul.")
	f.DurationVar(&cfg.HTTPClientTimeout, prefix+"consul.client-timeout", 2*longPollDuration, "HTTP timeout when talking to Consul")
	f.BoolVar(&cfg.ConsistentReads, prefix+"consul.consistent-reads", false, "Enable consistent reads to Consul.")
	f.Float64Var(&cfg.WatchKeyRateLimit, prefix+"consul.watch-rate-limit", 1, "Rate limit when watching key or prefix in Consul, in requests per second. 0 disables the rate limit.")
	f.IntVar(&cfg.WatchKeyBurstSize, prefix+"consul.watch-burst-size", 1, "Burst size used in rate limit. Values less than 1 are treated as 1.")
}

// NewClient returns a new Client.
func NewClient(cfg Config, codec codec.Codec) (*Client, error) {
	client, err := consul.NewClient(&consul.Config{
		Address: cfg.Host,
		Token:   cfg.ACLToken,
		Scheme:  "http",
		HttpClient: &http.Client{
			Transport: cleanhttp.DefaultPooledTransport(),
			// See https://blog.cloudflare.com/the-complete-guide-to-golang-net-http-timeouts/
			Timeout: cfg.HTTPClientTimeout,
		},
	})
	if err != nil {
		return nil, err
	}
	c := &Client{
		kv:    consulMetrics{client.KV()},
		codec: codec,
		cfg:   cfg,
	}
	return c, nil
}

// Put is mostly here for testing.
func (c *Client) Put(ctx context.Context, key string, value interface{}) error {
	bytes, err := c.codec.Encode(value)
	if err != nil {
		return err
	}

	return instrument.CollectedRequest(ctx, "Put", consulRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		_, err := c.kv.Put(&consul.KVPair{
			Key:   key,
			Value: bytes,
		}, nil)
		return err
	})
}

// CAS atomically modifies a value in a callback.
// If value doesn't exist you'll get nil as an argument to your callback.
func (c *Client) CAS(ctx context.Context, key string, f func(in interface{}) (out interface{}, retry bool, err error)) error {
	return instrument.CollectedRequest(ctx, "CAS loop", consulRequestDuration, instrument.ErrorCode, func(ctx context.Context) error {
		return c.cas(ctx, key, f)
	})
}

func (c *Client) cas(ctx context.Context, key string, f func(in interface{}) (out interface{}, retry bool, err error)) error {
	var (
		index   = uint64(0)
		retries = 10
	)
	for i := 0; i < retries; i++ {
		options := &consul.QueryOptions{
			AllowStale:        !c.cfg.ConsistentReads,
			RequireConsistent: c.cfg.ConsistentReads,
		}
		kvp, _, err := c.kv.Get(key, options.WithContext(ctx))
		if err != nil {
			level.Error(util.Logger).Log("msg", "error getting key", "key", key, "err", err)
			continue
		}
		var intermediate interface{}
		if kvp != nil {
			out, err := c.codec.Decode(kvp.Value)
			if err != nil {
				level.Error(util.Logger).Log("msg", "error decoding key", "key", key, "err", err)
				continue
			}
			// If key doesn't exist, index will be 0.
			index = kvp.ModifyIndex
			intermediate = out
		}

		intermediate, retry, err := f(intermediate)
		if err != nil {
			if !retry {
				return err
			}
			continue
		}

		// Treat the callback returning nil for intermediate as a decision to
		// not actually write to Consul, but this is not an error.
		if intermediate == nil {
			return nil
		}

		bytes, err := c.codec.Encode(intermediate)
		if err != nil {
			level.Error(util.Logger).Log("msg", "error serialising value", "key", key, "err", err)
			continue
		}
		ok, _, err := c.kv.CAS(&consul.KVPair{
			Key:         key,
			Value:       bytes,
			ModifyIndex: index,
		}, writeOptions.WithContext(ctx))
		if err != nil {
			level.Error(util.Logger).Log("msg", "error CASing", "key", key, "err", err)
			continue
		}
		if !ok {
			level.Debug(util.Logger).Log("msg", "error CASing, trying again", "key", key, "index", index)
			continue
		}
		return nil
	}
	return fmt.Errorf("failed to CAS %s", key)
}

// WatchKey will watch a given key in consul for changes. When the value
// under said key changes, the f callback is called with the deserialised
// value. To construct the deserialised value, a factory function should be
// supplied which generates an empty struct for WatchKey to deserialise
// into. This function blocks until the context is cancelled or f returns false.
func (c *Client) WatchKey(ctx context.Context, key string, f func(interface{}) bool) {
	var (
		backoff = util.NewBackoff(ctx, backoffConfig)
		index   = uint64(0)
		limiter = c.createRateLimiter()
	)

	for backoff.Ongoing() {
		err := limiter.Wait(ctx)
		if err != nil {
			level.Error(util.Logger).Log("msg", "error while rate-limiting", "key", key, "err", err)
			backoff.Wait()
			continue
		}

		queryOptions := &consul.QueryOptions{
			AllowStale:        !c.cfg.ConsistentReads,
			RequireConsistent: c.cfg.ConsistentReads,
			WaitIndex:         index,
			WaitTime:          longPollDuration,
		}

		kvp, meta, err := c.kv.Get(key, queryOptions.WithContext(ctx))

		// Don't backoff if value is not found (kvp == nil). In that case, Consul still returns index value,
		// and next call to Get will block as expected. We handle missing value below.
		if err != nil {
			level.Error(util.Logger).Log("msg", "error getting path", "key", key, "err", err)
			backoff.Wait()
			continue
		}
		backoff.Reset()

		skip := false
		index, skip = checkLastIndex(index, meta.LastIndex)
		if skip {
			continue
		}

		if kvp == nil {
			level.Info(util.Logger).Log("msg", "value is nil", "key", key, "index", index)
			continue
		}

		out, err := c.codec.Decode(kvp.Value)
		if err != nil {
			level.Error(util.Logger).Log("msg", "error decoding key", "key", key, "err", err)
			continue
		}
		if !f(out) {
			return
		}
	}
}

// WatchPrefix will watch a given prefix in Consul for new keys and changes to existing keys under that prefix.
// When the value under said key changes, the f callback is called with the deserialised value.
// Values in Consul are assumed to be JSON. This function blocks until the context is cancelled.
func (c *Client) WatchPrefix(ctx context.Context, prefix string, f func(string, interface{}) bool) {
	var (
		backoff = util.NewBackoff(ctx, backoffConfig)
		index   = uint64(0)
		limiter = c.createRateLimiter()
	)
	for backoff.Ongoing() {
		err := limiter.Wait(ctx)
		if err != nil {
			level.Error(util.Logger).Log("msg", "error while rate-limiting", "prefix", prefix, "err", err)
			backoff.Wait()
			continue
		}

		queryOptions := &consul.QueryOptions{
			AllowStale:        !c.cfg.ConsistentReads,
			RequireConsistent: c.cfg.ConsistentReads,
			WaitIndex:         index,
			WaitTime:          longPollDuration,
		}

		kvps, meta, err := c.kv.List(prefix, queryOptions.WithContext(ctx))
		// kvps being nil here is not an error -- quite the opposite. Consul returns index,
		// which makes next query blocking, so there is no need to detect this and act on it.
		if err != nil {
			level.Error(util.Logger).Log("msg", "error getting path", "prefix", prefix, "err", err)
			backoff.Wait()
			continue
		}
		backoff.Reset()

		newIndex, skip := checkLastIndex(index, meta.LastIndex)
		if skip {
			continue
		}

		for _, kvp := range kvps {
			// We asked for values newer than 'index', but Consul returns all values below given prefix,
			// even those that haven't changed. We don't need to report all of them as updated.
			if index > 0 && kvp.ModifyIndex <= index && kvp.CreateIndex <= index {
				continue
			}

			out, err := c.codec.Decode(kvp.Value)
			if err != nil {
				level.Error(util.Logger).Log("msg", "error decoding list of values for prefix:key", "prefix", prefix, "key", kvp.Key, "err", err)
				continue
			}
			if !f(kvp.Key, out) {
				return
			}
		}

		index = newIndex
	}
}

// List implements kv.List.
func (c *Client) List(ctx context.Context, prefix string) ([]string, error) {
	options := &consul.QueryOptions{
		AllowStale:        !c.cfg.ConsistentReads,
		RequireConsistent: c.cfg.ConsistentReads,
	}
	pairs, _, err := c.kv.List(prefix, options.WithContext(ctx))
	if err != nil {
		return nil, err
	}

	keys := make([]string, 0, len(pairs))
	for _, kvp := range pairs {
		keys = append(keys, kvp.Key)
	}
	return keys, nil
}

// Get implements kv.Get.
func (c *Client) Get(ctx context.Context, key string) (interface{}, error) {
	options := &consul.QueryOptions{
		AllowStale:        !c.cfg.ConsistentReads,
		RequireConsistent: c.cfg.ConsistentReads,
	}
	kvp, _, err := c.kv.Get(key, options.WithContext(ctx))
	if err != nil {
		return nil, err
	} else if kvp == nil {
		return nil, nil
	}
	return c.codec.Decode(kvp.Value)
}

// Delete implements kv.Delete.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.kv.Delete(key, writeOptions.WithContext(ctx))
	return err
}

func checkLastIndex(index, metaLastIndex uint64) (newIndex uint64, skip bool) {
	// See https://www.consul.io/api/features/blocking.html#implementation-details for logic behind these checks.
	if metaLastIndex == 0 {
		// Don't just keep using index=0.
		// After blocking request, returned index must be at least 1.
		return 1, false
	} else if metaLastIndex < index {
		// Index reset.
		return 0, false
	} else if index == metaLastIndex {
		// Skip if the index is the same as last time, because the key value is
		// guaranteed to be the same as last time
		return metaLastIndex, true
	} else {
		return metaLastIndex, false
	}
}

func (c *Client) createRateLimiter() *rate.Limiter {
	if c.cfg.WatchKeyRateLimit <= 0 {
		// burst is ignored when limit = rate.Inf
		return rate.NewLimiter(rate.Inf, 0)
	}
	burst := c.cfg.WatchKeyBurstSize
	if burst < 1 {
		burst = 1
	}
	return rate.NewLimiter(rate.Limit(c.cfg.WatchKeyRateLimit), burst)
}
