package kv

import (
	"context"
	"flag"
	"fmt"
	"sync"

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

// StoreConfig is a configuration used for building single store client, either
// Consul, Etcd, Memberlist or MultiClient. It was extracted from Config to keep
// single-client config separate from final client-config (with all the wrappers)
type StoreConfig struct {
	Consul consul.Config `yaml:"consul,omitempty"`
	Etcd   etcd.Config   `yaml:"etcd,omitempty"`
	Multi  MultiConfig   `yaml:"multi,omitempty"`

	// Function that returns memberlist.KV store to use. By using a function, we can delay
	// initialization of memberlist.KV until it is actually required.
	MemberlistKV func() (*memberlist.KV, error) `yaml:"-"`
}

// Config is config for a KVStore currently used by ring and HA tracker,
// where store can be consul or inmemory.
type Config struct {
	Store       string `yaml:"store,omitempty"`
	Prefix      string `yaml:"prefix,omitempty"`
	StoreConfig `yaml:",inline"`

	Mock Client `yaml:"-"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet.
// If prefix is an empty string we will register consul flags with no prefix and the
// store flag with the prefix ring, so ring.store. For everything else we pass the prefix
// to the Consul flags.
// If prefix is not an empty string it should end with a period.
func (cfg *Config) RegisterFlagsWithPrefix(flagsPrefix, defaultPrefix string, f *flag.FlagSet) {
	// We need Consul flags to not have the ring prefix to maintain compatibility.
	// This needs to be fixed in the future (1.0 release maybe?) when we normalize flags.
	// At the moment we have consul.<flag-name>, and ring.store, going forward it would
	// be easier to have everything under ring, so ring.consul.<flag-name>
	cfg.Consul.RegisterFlags(f, flagsPrefix)
	cfg.Etcd.RegisterFlagsWithPrefix(f, flagsPrefix)
	cfg.Multi.RegisterFlagsWithPrefix(f, flagsPrefix)

	if flagsPrefix == "" {
		flagsPrefix = "ring."
	}
	f.StringVar(&cfg.Prefix, flagsPrefix+"prefix", defaultPrefix, "The prefix for the keys in the store. Should end with a /.")
	f.StringVar(&cfg.Store, flagsPrefix+"store", "consul", "Backend storage to use for the ring. Supported values are: consul, etcd, inmemory, multi, memberlist (experimental).")
}

// Client is a high-level client for key-value stores (such as Etcd and
// Consul) that exposes operations such as CAS and Watch which take callbacks.
// It also deals with serialisation by using a Codec and having a instance of
// the the desired type passed in to methods ala json.Unmarshal.
type Client interface {
	// Get a specific key.  Will use a codec to deserialise key to appropriate type.
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
}

// NewClient creates a new Client (consul, etcd or inmemory) based on the config,
// encodes and decodes data for storage using the codec.
func NewClient(cfg Config, codec codec.Codec) (Client, error) {
	if cfg.Mock != nil {
		return cfg.Mock, nil
	}

	return createClient(cfg.Store, cfg.Prefix, cfg.StoreConfig, codec)
}

func createClient(name string, prefix string, cfg StoreConfig, codec codec.Codec) (Client, error) {
	var client Client
	var err error

	switch name {
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
		kv, err := cfg.MemberlistKV()
		if err != nil {
			return nil, err
		}
		client, err = memberlist.NewClient(kv, codec)
		if err != nil {
			return nil, err
		}

	case "multi":
		client, err = buildMultiClient(cfg, codec)

	default:
		return nil, fmt.Errorf("invalid KV store type: %s", name)
	}

	if err != nil {
		return nil, err
	}

	if prefix != "" {
		client = PrefixClient(client, prefix)
	}

	return metrics{client}, nil
}

func buildMultiClient(cfg StoreConfig, codec codec.Codec) (Client, error) {
	if cfg.Multi.Primary == "" || cfg.Multi.Secondary == "" {
		return nil, fmt.Errorf("primary or secondary store not set")
	}
	if cfg.Multi.Primary == "multi" || cfg.Multi.Secondary == "multi" {
		return nil, fmt.Errorf("primary and secondary stores cannot be multi-stores")
	}
	if cfg.Multi.Primary == cfg.Multi.Secondary {
		return nil, fmt.Errorf("primary and secondary stores must be different")
	}

	primary, err := createClient(cfg.Multi.Primary, "", cfg, codec)
	if err != nil {
		return nil, err
	}

	secondary, err := createClient(cfg.Multi.Secondary, "", cfg, codec)
	if err != nil {
		return nil, err
	}

	clients := []kvclient{
		{client: primary, name: cfg.Multi.Primary},
		{client: secondary, name: cfg.Multi.Secondary},
	}

	return NewMultiClient(cfg.Multi, clients), nil
}
