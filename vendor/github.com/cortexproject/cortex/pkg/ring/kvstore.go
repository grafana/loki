package ring

import (
	"context"
	"flag"
	"fmt"
	"sync"

	"github.com/golang/protobuf/proto"
	"github.com/golang/snappy"
)

var inmemoryStoreInit sync.Once
var inmemoryStore KVClient

// KVClient is a high-level client for Consul, that exposes operations
// such as CAS and Watch which take callbacks.  It also deals with serialisation
// by having an instance factory passed in to methods and deserialising into that.
type KVClient interface {
	CAS(ctx context.Context, key string, f CASCallback) error
	WatchKey(ctx context.Context, key string, f func(interface{}) bool)
	WatchPrefix(ctx context.Context, prefix string, f func(string, interface{}) bool)
	Get(ctx context.Context, key string) (interface{}, error)
	PutBytes(ctx context.Context, key string, buf []byte) error
}

// KVConfig is config for a KVStore currently used by ring and HA tracker,
// where store can be consul or inmemory.
type KVConfig struct {
	Store  string       `yaml:"store,omitempty"`
	Consul ConsulConfig `yaml:"consul,omitempty"`

	Mock KVClient
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet.
// If prefix is an empty string we will register consul flags with no prefix and the
// store flag with the prefix ring, so ring.store. For everything else we pass the prefix
// to the Consul flags.
// If prefix is not an empty string it should end with a period.
func (cfg *KVConfig) RegisterFlagsWithPrefix(prefix string, f *flag.FlagSet) {
	// We need Consul flags to not have the ring prefix to maintain compatibility.
	// This needs to be fixed in the future (1.0 release maybe?) when we normalize flags.
	// At the moment we have consul.<flag-name>, and ring.store, going forward it would
	// be easier to have everything under ring, so ring.consul.<flag-name>
	cfg.Consul.RegisterFlags(f, prefix)
	if prefix == "" {
		prefix = "ring."
	}
	f.StringVar(&cfg.Store, prefix+"store", "consul", "Backend storage to use for the ring (consul, inmemory).")
}

// CASCallback is the type of the callback to CAS.  If err is nil, out must be non-nil.
type CASCallback func(in interface{}) (out interface{}, retry bool, err error)

// NewKVStore creates a new KVstore client (inmemory or consul) based on the config,
// encodes and decodes data for storage using the codec.
func NewKVStore(cfg KVConfig, codec Codec) (KVClient, error) {
	if cfg.Mock != nil {
		return cfg.Mock, nil
	}

	switch cfg.Store {
	case "consul":
		return NewConsulClient(cfg.Consul, codec)
	case "inmemory":
		// If we use the in-memory store, make sure everyone gets the same instance
		// within the same process.
		inmemoryStoreInit.Do(func() {
			inmemoryStore = NewInMemoryKVClient(codec)
		})
		return inmemoryStore, nil
	default:
		return nil, fmt.Errorf("invalid KV store type: %s", cfg.Store)
	}
}

// Codec allows the consul client to serialise and deserialise values.
type Codec interface {
	Decode([]byte) (interface{}, error)
	Encode(interface{}) ([]byte, error)
}

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
