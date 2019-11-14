package kv

import (
	"context"
	"flag"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/ring/kv/codec"
	"github.com/cortexproject/cortex/pkg/ring/kv/consul"
	"github.com/cortexproject/cortex/pkg/ring/kv/etcd"
	"github.com/cortexproject/cortex/pkg/ring/kv/memberlist"
)

// The NewInMemoryKVClient returned by NewClient() is a singleton, so
// that distributors and ingesters started in the same process can
// find themselves.
var inmemoryStoreInit sync.Once
var inmemoryStore Client

// Config is config for a KVStore currently used by ring and HA tracker,
// where store can be consul or inmemory.
type Config struct {
	Store      string            `yaml:"store,omitempty"`
	Consul     consul.Config     `yaml:"consul,omitempty"`
	Etcd       etcd.Config       `yaml:"etcd,omitempty"`
	Memberlist memberlist.Config `yaml:"memberlist,omitempty"`
	Prefix     string            `yaml:"prefix,omitempty"`

	Mock Client
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet.
// If prefix is an empty string we will register consul flags with no prefix and the
// store flag with the prefix ring, so ring.store. For everything else we pass the prefix
// to the Consul flags.
// If prefix is not an empty string it should end with a period.
func (cfg *Config) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	// We need Consul flags to not have the ring prefix to maintain compatibility.
	// This needs to be fixed in the future (1.0 release maybe?) when we normalize flags.
	// At the moment we have consul.<flag-name>, and ring.store, going forward it would
	// be easier to have everything under ring, so ring.consul.<flag-name>
	cfg.Consul.RegisterFlags(f, prefix)
	cfg.Etcd.RegisterFlagsWithPrefix(f, prefix)
	cfg.Memberlist.RegisterFlags(f, prefix)
	if prefix == "" {
		prefix = "ring."
	}
	f.StringVar(&cfg.Prefix, prefix+"prefix", "collectors/", "The prefix for the keys in the store. Should end with a /.")
	f.StringVar(&cfg.Store, prefix+"store", "consul", "Backend storage to use for the ring (consul, etcd, inmemory, memberlist [experimental]).")
}

// Client is a high-level client for key-value stores (such as Etcd and
// Consul) that exposes operations such as CAS and Watch which take callbacks.
// It also deals with serialisation by using a Codec and having a instance of
// the the desired type passed in to methods ala json.Unmarshal.
type Client interface {
	// Get a spefic key.  Will use a codec to deserialise key to appropriate type.
	Get(ctx context.Context, key string) (interface{}, error)

	// CAS stands for Compare-And-Swap.  Will call provided callback f with the
	// current value of the key and allow callback to return a different value.
	// Will then attempt to atomically swap the current value for the new value.
	// If that doesn't succeed will try again - callback will be called again
	// with new value etc.  Guarantees that only a single concurrent CAS
	// succeeds.  Callback can return nil to indicate it is happy with existing
	// value.
	CAS(ctx context.Context, key string, f func(in interface{}) (out interface{}, retry bool, err error)) error

	// WatchKey calls f whenever the value stored under key changes.
	WatchKey(ctx context.Context, key string, f func(interface{}) bool)

	// WatchPrefix calls f whenever any value stored under prefix changes.
	WatchPrefix(ctx context.Context, prefix string, f func(string, interface{}) bool)

	// If client needs to do some cleanup, it can do it here.
	Stop()
}

// NewClient creates a new Client (consul, etcd or inmemory) based on the config,
// encodes and decodes data for storage using the codec.
func NewClient(cfg Config, codec codec.Codec) (Client, error) {
	if cfg.Mock != nil {
		return cfg.Mock, nil
	}

	var client Client
	var err error

	switch cfg.Store {
	case "consul":
		client, err = consul.NewClient(cfg.Consul, codec)

	case "etcd":
		client, err = etcd.New(cfg.Etcd, codec)

	case "inmemory":
		// If we use the in-memory store, make sure everyone gets the same instance
		// within the same process.
		inmemoryStoreInit.Do(func() {
			inmemoryStore = consul.NewInMemoryClient(codec)
		})
		client = inmemoryStore

	case "memberlist":
		cfg.Memberlist.MetricsRegisterer = prometheus.DefaultRegisterer
		client, err = memberlist.NewMemberlistClient(cfg.Memberlist, codec)

	default:
		return nil, fmt.Errorf("invalid KV store type: %s", cfg.Store)
	}

	if err != nil {
		return nil, err
	}

	if cfg.Prefix != "" {
		client = PrefixClient(client, cfg.Prefix)
	}

	return metrics{client}, nil
}
