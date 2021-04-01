// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package storecache

import (
	"regexp"
	"strings"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"gopkg.in/yaml.v2"

	"github.com/thanos-io/thanos/pkg/block/metadata"
	cache "github.com/thanos-io/thanos/pkg/cache"
	"github.com/thanos-io/thanos/pkg/cacheutil"
	"github.com/thanos-io/thanos/pkg/model"
	"github.com/thanos-io/thanos/pkg/objstore"
)

// BucketCacheProvider is a type used to evaluate all bucket cache providers.
type BucketCacheProvider string

const (
	InMemoryBucketCacheProvider  BucketCacheProvider = "IN-MEMORY" // In-memory cache-provider for caching bucket.
	MemcachedBucketCacheProvider BucketCacheProvider = "MEMCACHED" // Memcached cache-provider for caching bucket.
)

// CachingWithBackendConfig is a configuration of caching bucket used by Store component.
type CachingWithBackendConfig struct {
	Type          BucketCacheProvider `yaml:"type"`
	BackendConfig interface{}         `yaml:"config"`

	// Basic unit used to cache chunks.
	ChunkSubrangeSize int64 `yaml:"chunk_subrange_size"`

	// Maximum number of GetRange requests issued by this bucket for single GetRange call. Zero or negative value = unlimited.
	MaxChunksGetRangeRequests int `yaml:"max_chunks_get_range_requests"`

	// TTLs for various cache items.
	ChunkObjectAttrsTTL time.Duration `yaml:"chunk_object_attrs_ttl"`
	ChunkSubrangeTTL    time.Duration `yaml:"chunk_subrange_ttl"`

	// How long to cache result of Iter call in root directory.
	BlocksIterTTL time.Duration `yaml:"blocks_iter_ttl"`

	// Config for Exists and Get operations for metadata files.
	MetafileExistsTTL      time.Duration `yaml:"metafile_exists_ttl"`
	MetafileDoesntExistTTL time.Duration `yaml:"metafile_doesnt_exist_ttl"`
	MetafileContentTTL     time.Duration `yaml:"metafile_content_ttl"`
	MetafileMaxSize        model.Bytes   `yaml:"metafile_max_size"`
}

func (cfg *CachingWithBackendConfig) Defaults() {
	cfg.ChunkSubrangeSize = 16000 // Equal to max chunk size.
	cfg.ChunkObjectAttrsTTL = 24 * time.Hour
	cfg.ChunkSubrangeTTL = 24 * time.Hour
	cfg.MaxChunksGetRangeRequests = 3
	cfg.BlocksIterTTL = 5 * time.Minute
	cfg.MetafileExistsTTL = 2 * time.Hour
	cfg.MetafileDoesntExistTTL = 15 * time.Minute
	cfg.MetafileContentTTL = 24 * time.Hour
	cfg.MetafileMaxSize = 1024 * 1024 // Equal to default MaxItemSize in memcached client.
}

// NewCachingBucketFromYaml uses YAML configuration to create new caching bucket.
func NewCachingBucketFromYaml(yamlContent []byte, bucket objstore.Bucket, logger log.Logger, reg prometheus.Registerer) (objstore.InstrumentedBucket, error) {
	level.Info(logger).Log("msg", "loading caching bucket configuration")

	config := &CachingWithBackendConfig{}
	config.Defaults()

	if err := yaml.UnmarshalStrict(yamlContent, config); err != nil {
		return nil, errors.Wrap(err, "parsing config YAML file")
	}

	backendConfig, err := yaml.Marshal(config.BackendConfig)
	if err != nil {
		return nil, errors.Wrap(err, "marshal content of cache backend configuration")
	}

	var c cache.Cache

	switch strings.ToUpper(string(config.Type)) {
	case string(MemcachedBucketCacheProvider):
		var memcached cacheutil.MemcachedClient
		memcached, err := cacheutil.NewMemcachedClient(logger, "caching-bucket", backendConfig, reg)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create memcached client")
		}
		c = cache.NewMemcachedCache("caching-bucket", logger, memcached, reg)
	case string(InMemoryBucketCacheProvider):
		c, err = cache.NewInMemoryCache("caching-bucket", logger, reg, backendConfig)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to create inmemory cache")
		}
	default:
		return nil, errors.Errorf("unsupported cache type: %s", config.Type)
	}

	// Include interactions with cache in the traces.
	c = cache.NewTracingCache(c)
	cfg := NewCachingBucketConfig()

	// Configure cache.
	cfg.CacheGetRange("chunks", c, isTSDBChunkFile, config.ChunkSubrangeSize, config.ChunkObjectAttrsTTL, config.ChunkSubrangeTTL, config.MaxChunksGetRangeRequests)
	cfg.CacheExists("meta.jsons", c, isMetaFile, config.MetafileExistsTTL, config.MetafileDoesntExistTTL)
	cfg.CacheGet("meta.jsons", c, isMetaFile, int(config.MetafileMaxSize), config.MetafileContentTTL, config.MetafileExistsTTL, config.MetafileDoesntExistTTL)

	// Cache Iter requests for root.
	cfg.CacheIter("blocks-iter", c, isBlocksRootDir, config.BlocksIterTTL, JSONIterCodec{})

	cb, err := NewCachingBucket(bucket, cfg, logger, reg)
	if err != nil {
		return nil, err
	}

	return cb, nil
}

var chunksMatcher = regexp.MustCompile(`^.*/chunks/\d+$`)

func isTSDBChunkFile(name string) bool { return chunksMatcher.MatchString(name) }

func isMetaFile(name string) bool {
	return strings.HasSuffix(name, "/"+metadata.MetaFilename) || strings.HasSuffix(name, "/"+metadata.DeletionMarkFilename)
}

func isBlocksRootDir(name string) bool {
	return name == ""
}
