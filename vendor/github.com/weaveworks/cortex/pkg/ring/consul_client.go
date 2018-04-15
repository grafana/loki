package ring

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/golang/protobuf/proto"
	"github.com/golang/snappy"
	consul "github.com/hashicorp/consul/api"
	cleanhttp "github.com/hashicorp/go-cleanhttp"

	"github.com/weaveworks/cortex/pkg/util"
)

const (
	longPollDuration = 10 * time.Second
)

// ConsulConfig to create a ConsulClient
type ConsulConfig struct {
	Host              string
	Prefix            string
	HTTPClientTimeout time.Duration
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *ConsulConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Host, "consul.hostname", "localhost:8500", "Hostname and port of Consul.")
	f.StringVar(&cfg.Prefix, "consul.prefix", "collectors/", "Prefix for keys in Consul.")
	f.DurationVar(&cfg.HTTPClientTimeout, "consul.client-timeout", 2*longPollDuration, "HTTP timeout when talking to consul")
}

// KVClient is a high-level client for Consul, that exposes operations
// such as CAS and Watch which take callbacks.  It also deals with serialisation
// by having an instance factory passed in to methods and deserialising into that.
type KVClient interface {
	CAS(key string, f CASCallback) error
	WatchKey(ctx context.Context, key string, f func(interface{}) bool)
	Get(key string) (interface{}, error)
	PutBytes(key string, buf []byte) error
}

// CASCallback is the type of the callback to CAS.  If err is nil, out must be non-nil.
type CASCallback func(in interface{}) (out interface{}, retry bool, err error)

// Codec allows the consult client to serialise and deserialise values.
type Codec interface {
	Decode([]byte) (interface{}, error)
	Encode(interface{}) ([]byte, error)
}

type kv interface {
	CAS(p *consul.KVPair, q *consul.WriteOptions) (bool, *consul.WriteMeta, error)
	Get(key string, q *consul.QueryOptions) (*consul.KVPair, *consul.QueryMeta, error)
	List(path string, q *consul.QueryOptions) (consul.KVPairs, *consul.QueryMeta, error)
	Put(p *consul.KVPair, q *consul.WriteOptions) (*consul.WriteMeta, error)
}

type consulClient struct {
	kv
	codec            Codec
	longPollDuration time.Duration
}

// NewConsulClient returns a new ConsulClient.
func NewConsulClient(cfg ConsulConfig, codec Codec) (KVClient, error) {
	client, err := consul.NewClient(&consul.Config{
		Address: cfg.Host,
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
	var c KVClient = &consulClient{
		kv:               client.KV(),
		codec:            codec,
		longPollDuration: longPollDuration,
	}
	if cfg.Prefix != "" {
		c = PrefixClient(c, cfg.Prefix)
	}
	return c, nil
}

var (
	queryOptions = &consul.QueryOptions{
		RequireConsistent: true,
	}
	writeOptions = &consul.WriteOptions{}

	// ErrNotFound is returned by ConsulClient.Get.
	ErrNotFound = fmt.Errorf("Not found")
)

// ProtoCodec is a Codec for proto/snappy
type ProtoCodec struct {
	Factory func() proto.Message
}

// Decode implements Codec
func (p ProtoCodec) Decode(bytes []byte) (interface{}, error) {
	out := p.Factory()
	bytes, err := snappy.Decode(nil, bytes)
	if err != nil {
		return nil, err
	}
	if err := proto.Unmarshal(bytes, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Encode implements Codec
func (p ProtoCodec) Encode(msg interface{}) ([]byte, error) {
	bytes, err := proto.Marshal(msg.(proto.Message))
	if err != nil {
		return nil, err
	}
	return snappy.Encode(nil, bytes), nil
}

// CAS atomically modifies a value in a callback.
// If value doesn't exist you'll get nil as an argument to your callback.
func (c *consulClient) CAS(key string, f CASCallback) error {
	var (
		index   = uint64(0)
		retries = 10
		retry   = true
	)
	for i := 0; i < retries; i++ {
		kvp, _, err := c.kv.Get(key, queryOptions)
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

		intermediate, retry, err = f(intermediate)
		if err != nil {
			level.Error(util.Logger).Log("msg", "error CASing", "key", key, "err", err)
			if !retry {
				return err
			}
			continue
		}

		if intermediate == nil {
			panic("Callback must instantiate value!")
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
		}, writeOptions)
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

var backoffConfig = util.BackoffConfig{
	MinBackoff: 1 * time.Second,
	MaxBackoff: 1 * time.Minute,
}

// WatchKey will watch a given key in consul for changes. When the value
// under said key changes, the f callback is called with the deserialised
// value. To construct the deserialised value, a factory function should be
// supplied which generates an empty struct for WatchKey to deserialise
// into. Values in Consul are assumed to be JSON. This function blocks until
// the context is cancelled.
func (c *consulClient) WatchKey(ctx context.Context, key string, f func(interface{}) bool) {
	var (
		backoff = util.NewBackoff(ctx, backoffConfig)
		index   = uint64(0)
	)
	for backoff.Ongoing() {
		kvp, meta, err := c.kv.Get(key, &consul.QueryOptions{
			RequireConsistent: true,
			WaitIndex:         index,
			WaitTime:          c.longPollDuration,
		})
		if err != nil || kvp == nil {
			level.Error(util.Logger).Log("msg", "error getting path", "key", key, "err", err)
			backoff.Wait()
			continue
		}
		backoff.Reset()

		// Skip if the index is the same as last time, because the key value is
		// guaranteed to be the same as last time
		if index == meta.LastIndex {
			continue
		}
		index = meta.LastIndex

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

func (c *consulClient) PutBytes(key string, buf []byte) error {
	_, err := c.kv.Put(&consul.KVPair{
		Key:   key,
		Value: buf,
	}, &consul.WriteOptions{})
	return err
}

func (c *consulClient) Get(key string) (interface{}, error) {
	kvp, _, err := c.kv.Get(key, &consul.QueryOptions{})
	if err != nil {
		return nil, err
	}
	return c.codec.Decode(kvp.Value)
}

type prefixedConsulClient struct {
	prefix string
	consul KVClient
}

// PrefixClient takes a ConsulClient and forces a prefix on all its operations.
func PrefixClient(client KVClient, prefix string) KVClient {
	return &prefixedConsulClient{prefix, client}
}

// CAS atomically modifies a value in a callback. If the value doesn't exist,
// you'll get 'nil' as an argument to your callback.
func (c *prefixedConsulClient) CAS(key string, f CASCallback) error {
	return c.consul.CAS(c.prefix+key, f)
}

// WatchKey watches a key.
func (c *prefixedConsulClient) WatchKey(ctx context.Context, key string, f func(interface{}) bool) {
	c.consul.WatchKey(ctx, c.prefix+key, f)
}

// PutBytes writes bytes to Consul.
func (c *prefixedConsulClient) PutBytes(key string, buf []byte) error {
	return c.consul.PutBytes(c.prefix+key, buf)
}

func (c *prefixedConsulClient) Get(key string) (interface{}, error) {
	return c.consul.Get(c.prefix + key)
}
