package storage

import (
	"context"
	"flag"
	"fmt"
	"strings"
	"time"

	"github.com/cortexproject/cortex/pkg/chunk"
	"github.com/cortexproject/cortex/pkg/chunk/aws"
	"github.com/cortexproject/cortex/pkg/chunk/cache"
	"github.com/cortexproject/cortex/pkg/chunk/cassandra"
	"github.com/cortexproject/cortex/pkg/chunk/gcp"
	"github.com/cortexproject/cortex/pkg/util"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
)

// Config chooses which storage client to use.
type Config struct {
	StorageClient          string
	AWSStorageConfig       aws.StorageConfig
	GCPStorageConfig       gcp.Config
	CassandraStorageConfig cassandra.Config

	IndexCacheSize     int
	IndexCacheValidity time.Duration
	memcacheClient     cache.MemcachedClientConfig

	indexQueriesCacheConfig cache.Config
}

// RegisterFlags adds the flags required to configure this flag set.
func (cfg *Config) RegisterFlags(f *flag.FlagSet) {
	flag.StringVar(&cfg.StorageClient, "chunk.storage-client", "aws", "Which storage client to use (aws, gcp, cassandra, inmemory).")
	cfg.AWSStorageConfig.RegisterFlags(f)
	cfg.GCPStorageConfig.RegisterFlags(f)
	cfg.CassandraStorageConfig.RegisterFlags(f)

	// Deprecated flags!!
	f.IntVar(&cfg.IndexCacheSize, "store.index-cache-size", 0, "Deprecated: Use -store.index-cache-read.*; Size of in-memory index cache, 0 to disable.")
	f.DurationVar(&cfg.IndexCacheValidity, "store.index-cache-validity", 5*time.Minute, "Deprecated: Use -store.index-cache-read.*; Period for which entries in the index cache are valid. Should be no higher than -ingester.max-chunk-idle.")
	cfg.memcacheClient.RegisterFlagsWithPrefix("index.", "Deprecated: Use -store.index-cache-read.*;", f)

	cfg.indexQueriesCacheConfig.RegisterFlagsWithPrefix("store.index-cache-read.", "Cache config for index entry reading. ", f)
}

// Opts makes the storage clients based on the configuration.
func Opts(cfg Config, schemaCfg chunk.SchemaConfig) ([]chunk.StorageOpt, error) {
	var tieredCache cache.Cache
	var err error

	// Building up from deprecated flags.
	var caches []cache.Cache
	if cfg.IndexCacheSize > 0 {
		fifocache := cache.Instrument("fifo-index", cache.NewFifoCache("index", cache.FifoCacheConfig{Size: cfg.IndexCacheSize, Validity: cfg.IndexCacheValidity}))
		caches = append(caches, fifocache)
	}
	if cfg.memcacheClient.Host != "" {
		client := cache.NewMemcachedClient(cfg.memcacheClient)
		memcache := cache.Instrument("memcache-index", cache.NewMemcached(cache.MemcachedConfig{
			Expiration: cfg.IndexCacheValidity,
		}, client))
		caches = append(caches, cache.NewBackground("memcache-index", cache.BackgroundConfig{
			WriteBackGoroutines: 10,
			WriteBackBuffer:     100,
		}, memcache))
	}

	if len(caches) > 0 {
		tieredCache = cache.NewTiered(caches)
		cfg.indexQueriesCacheConfig.DefaultValidity = cfg.IndexCacheValidity
	} else {
		tieredCache, err = cache.New(cfg.indexQueriesCacheConfig)
		if err != nil {
			return nil, err
		}
	}

	opts, err := newStorageOpts(cfg, schemaCfg)
	if err != nil {
		return nil, errors.Wrap(err, "error creating storage client")
	}

	for i := range opts {
		opts[i].Client = newCachingStorageClient(opts[i].Client, tieredCache, cfg.indexQueriesCacheConfig.DefaultValidity)
	}

	return opts, nil
}

func newStorageOpts(cfg Config, schemaCfg chunk.SchemaConfig) ([]chunk.StorageOpt, error) {
	switch cfg.StorageClient {
	case "inmemory":
		return chunk.Opts()
	case "aws":
		if cfg.AWSStorageConfig.DynamoDB.URL == nil {
			return nil, fmt.Errorf("Must set -dynamodb.url in aws mode")
		}
		path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			level.Warn(util.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
		}
		return aws.Opts(cfg.AWSStorageConfig, schemaCfg)
	case "gcp":
		return gcp.Opts(context.Background(), cfg.GCPStorageConfig, schemaCfg)
	case "cassandra":
		return cassandra.Opts(cfg.CassandraStorageConfig, schemaCfg)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: aws, gcp, cassandra, inmemory", cfg.StorageClient)
	}
}

// NewTableClient makes a new table client based on the configuration.
func NewTableClient(cfg Config) (chunk.TableClient, error) {
	switch cfg.StorageClient {
	case "inmemory":
		return chunk.NewMockStorage(), nil
	case "aws":
		path := strings.TrimPrefix(cfg.AWSStorageConfig.DynamoDB.URL.Path, "/")
		if len(path) > 0 {
			level.Warn(util.Logger).Log("msg", "ignoring DynamoDB URL path", "path", path)
		}
		return aws.NewDynamoDBTableClient(cfg.AWSStorageConfig.DynamoDBConfig)
	case "gcp":
		return gcp.NewTableClient(context.Background(), cfg.GCPStorageConfig)
	case "cassandra":
		return cassandra.NewTableClient(context.Background(), cfg.CassandraStorageConfig)
	default:
		return nil, fmt.Errorf("Unrecognized storage client %v, choose one of: aws, gcp, inmemory", cfg.StorageClient)
	}
}
