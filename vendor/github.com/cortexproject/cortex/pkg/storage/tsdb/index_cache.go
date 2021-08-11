package tsdb

import (
	"flag"
	"fmt"
	"strings"

	"github.com/alecthomas/units"
	"github.com/go-kit/kit/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/cacheutil"
	"github.com/thanos-io/thanos/pkg/model"
	storecache "github.com/thanos-io/thanos/pkg/store/cache"

	"github.com/cortexproject/cortex/pkg/util"
)

const (
	// IndexCacheBackendInMemory is the value for the in-memory index cache backend.
	IndexCacheBackendInMemory = "inmemory"

	// IndexCacheBackendMemcached is the value for the memcached index cache backend.
	IndexCacheBackendMemcached = "memcached"

	// IndexCacheBackendDefault is the value for the default index cache backend.
	IndexCacheBackendDefault = IndexCacheBackendInMemory

	defaultMaxItemSize = model.Bytes(128 * units.MiB)
)

var (
	supportedIndexCacheBackends = []string{IndexCacheBackendInMemory, IndexCacheBackendMemcached}

	errUnsupportedIndexCacheBackend = errors.New("unsupported index cache backend")
	errNoIndexCacheAddresses        = errors.New("no index cache backend addresses")
)

type IndexCacheConfig struct {
	Backend   string                   `yaml:"backend"`
	InMemory  InMemoryIndexCacheConfig `yaml:"inmemory"`
	Memcached MemcachedClientConfig    `yaml:"memcached"`
}

func (cfg *IndexCacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "blocks-storage.bucket-store.index-cache.")
}

func (cfg *IndexCacheConfig) RegisterFlagsWithPrefix(f *flag.FlagSet, prefix string) {
	f.StringVar(&cfg.Backend, prefix+"backend", IndexCacheBackendDefault, fmt.Sprintf("The index cache backend type. Supported values: %s.", strings.Join(supportedIndexCacheBackends, ", ")))

	cfg.InMemory.RegisterFlagsWithPrefix(f, prefix+"inmemory.")
	cfg.Memcached.RegisterFlagsWithPrefix(f, prefix+"memcached.")
}

// Validate the config.
func (cfg *IndexCacheConfig) Validate() error {
	if !util.StringsContain(supportedIndexCacheBackends, cfg.Backend) {
		return errUnsupportedIndexCacheBackend
	}

	if cfg.Backend == IndexCacheBackendMemcached {
		if err := cfg.Memcached.Validate(); err != nil {
			return err
		}
	}

	return nil
}

type InMemoryIndexCacheConfig struct {
	MaxSizeBytes uint64 `yaml:"max_size_bytes"`
}

func (cfg *InMemoryIndexCacheConfig) RegisterFlagsWithPrefix(f *flag.FlagSet, prefix string) {
	f.Uint64Var(&cfg.MaxSizeBytes, prefix+"max-size-bytes", uint64(1*units.Gibibyte), "Maximum size in bytes of in-memory index cache used to speed up blocks index lookups (shared between all tenants).")
}

// NewIndexCache creates a new index cache based on the input configuration.
func NewIndexCache(cfg IndexCacheConfig, logger log.Logger, registerer prometheus.Registerer) (storecache.IndexCache, error) {
	switch cfg.Backend {
	case IndexCacheBackendInMemory:
		return newInMemoryIndexCache(cfg.InMemory, logger, registerer)
	case IndexCacheBackendMemcached:
		return newMemcachedIndexCache(cfg.Memcached, logger, registerer)
	default:
		return nil, errUnsupportedIndexCacheBackend
	}
}

func newInMemoryIndexCache(cfg InMemoryIndexCacheConfig, logger log.Logger, registerer prometheus.Registerer) (storecache.IndexCache, error) {
	maxCacheSize := model.Bytes(cfg.MaxSizeBytes)

	// Calculate the max item size.
	maxItemSize := defaultMaxItemSize
	if maxItemSize > maxCacheSize {
		maxItemSize = maxCacheSize
	}

	return storecache.NewInMemoryIndexCacheWithConfig(logger, registerer, storecache.InMemoryIndexCacheConfig{
		MaxSize:     maxCacheSize,
		MaxItemSize: maxItemSize,
	})
}

func newMemcachedIndexCache(cfg MemcachedClientConfig, logger log.Logger, registerer prometheus.Registerer) (storecache.IndexCache, error) {
	client, err := cacheutil.NewMemcachedClientWithConfig(logger, "index-cache", cfg.ToMemcachedClientConfig(), registerer)
	if err != nil {
		return nil, errors.Wrapf(err, "create index cache memcached client")
	}

	return storecache.NewMemcachedIndexCache(logger, client, registerer)
}
