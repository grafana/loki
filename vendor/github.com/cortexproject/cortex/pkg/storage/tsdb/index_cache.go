package tsdb

import (
	"flag"
	"fmt"
	"strings"
	"time"

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
	Backend             string                    `yaml:"backend"`
	InMemory            InMemoryIndexCacheConfig  `yaml:"inmemory"`
	Memcached           MemcachedIndexCacheConfig `yaml:"memcached"`
	PostingsCompression bool                      `yaml:"postings_compression_enabled"`
}

func (cfg *IndexCacheConfig) RegisterFlags(f *flag.FlagSet) {
	cfg.RegisterFlagsWithPrefix(f, "experimental.tsdb.bucket-store.index-cache.")
}

func (cfg *IndexCacheConfig) RegisterFlagsWithPrefix(f *flag.FlagSet, prefix string) {
	f.StringVar(&cfg.Backend, prefix+"backend", IndexCacheBackendDefault, fmt.Sprintf("The index cache backend type. Supported values: %s.", strings.Join(supportedIndexCacheBackends, ", ")))
	f.BoolVar(&cfg.PostingsCompression, prefix+"postings-compression-enabled", false, "Compress postings before storing them to postings cache.")

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

type MemcachedIndexCacheConfig struct {
	Addresses              string        `yaml:"addresses"`
	Timeout                time.Duration `yaml:"timeout"`
	MaxIdleConnections     int           `yaml:"max_idle_connections"`
	MaxAsyncConcurrency    int           `yaml:"max_async_concurrency"`
	MaxAsyncBufferSize     int           `yaml:"max_async_buffer_size"`
	MaxGetMultiConcurrency int           `yaml:"max_get_multi_concurrency"`
	MaxGetMultiBatchSize   int           `yaml:"max_get_multi_batch_size"`
	MaxItemSize            int           `yaml:"max_item_size"`
}

func (cfg *MemcachedIndexCacheConfig) RegisterFlagsWithPrefix(f *flag.FlagSet, prefix string) {
	f.StringVar(&cfg.Addresses, prefix+"addresses", "", "Comma separated list of memcached addresses. Supported prefixes are: dns+ (looked up as an A/AAAA query), dnssrv+ (looked up as a SRV query, dnssrvnoa+ (looked up as a SRV query, with no A/AAAA lookup made after that).")
	f.DurationVar(&cfg.Timeout, prefix+"timeout", 100*time.Millisecond, "The socket read/write timeout.")
	f.IntVar(&cfg.MaxIdleConnections, prefix+"max-idle-connections", 16, "The maximum number of idle connections that will be maintained per address.")
	f.IntVar(&cfg.MaxAsyncConcurrency, prefix+"max-async-concurrency", 50, "The maximum number of concurrent asynchronous operations can occur.")
	f.IntVar(&cfg.MaxAsyncBufferSize, prefix+"max-async-buffer-size", 10000, "The maximum number of enqueued asynchronous operations allowed.")
	f.IntVar(&cfg.MaxGetMultiConcurrency, prefix+"max-get-multi-concurrency", 100, "The maximum number of concurrent connections running get operations. If set to 0, concurrency is unlimited.")
	f.IntVar(&cfg.MaxGetMultiBatchSize, prefix+"max-get-multi-batch-size", 0, "The maximum number of keys a single underlying get operation should run. If more keys are specified, internally keys are splitted into multiple batches and fetched concurrently, honoring the max concurrency. If set to 0, the max batch size is unlimited.")
	f.IntVar(&cfg.MaxItemSize, prefix+"max-item-size", 1024*1024, "The maximum size of an item stored in memcached. Bigger items are not stored. If set to 0, no maximum size is enforced.")
}

func (cfg *MemcachedIndexCacheConfig) GetAddresses() []string {
	if cfg.Addresses == "" {
		return []string{}
	}

	return strings.Split(cfg.Addresses, ",")
}

// Validate the config.
func (cfg *MemcachedIndexCacheConfig) Validate() error {
	if len(cfg.GetAddresses()) == 0 {
		return errNoIndexCacheAddresses
	}

	return nil
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

func newMemcachedIndexCache(cfg MemcachedIndexCacheConfig, logger log.Logger, registerer prometheus.Registerer) (storecache.IndexCache, error) {
	config := cacheutil.MemcachedClientConfig{
		Addresses:                 cfg.GetAddresses(),
		Timeout:                   cfg.Timeout,
		MaxIdleConnections:        cfg.MaxIdleConnections,
		MaxAsyncConcurrency:       cfg.MaxAsyncConcurrency,
		MaxAsyncBufferSize:        cfg.MaxAsyncBufferSize,
		MaxGetMultiConcurrency:    cfg.MaxGetMultiConcurrency,
		MaxGetMultiBatchSize:      cfg.MaxGetMultiBatchSize,
		MaxItemSize:               model.Bytes(cfg.MaxItemSize),
		DNSProviderUpdateInterval: 30 * time.Second,
	}

	client, err := cacheutil.NewMemcachedClientWithConfig(logger, "index-cache", config, registerer)
	if err != nil {
		return nil, errors.Wrapf(err, "create index cache memcached client")
	}

	return storecache.NewMemcachedIndexCache(logger, client, registerer)
}
