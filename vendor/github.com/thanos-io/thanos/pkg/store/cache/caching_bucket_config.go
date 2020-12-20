// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package storecache

import (
	"time"

	"github.com/thanos-io/thanos/pkg/cache"
	"github.com/thanos-io/thanos/pkg/objstore"
)

// Codec for encoding and decoding results of Iter call.
type IterCodec interface {
	Encode(files []string) ([]byte, error)
	Decode(cachedData []byte) ([]string, error)
}

// CachingBucketConfig contains low-level configuration for individual bucket operations.
// This is not exposed to the user, but it is expected that code sets up individual
// operations based on user-provided configuration.
type CachingBucketConfig struct {
	get        map[string]*getConfig
	iter       map[string]*iterConfig
	exists     map[string]*existsConfig
	getRange   map[string]*getRangeConfig
	attributes map[string]*attributesConfig
}

func NewCachingBucketConfig() *CachingBucketConfig {
	return &CachingBucketConfig{
		get:        map[string]*getConfig{},
		iter:       map[string]*iterConfig{},
		exists:     map[string]*existsConfig{},
		getRange:   map[string]*getRangeConfig{},
		attributes: map[string]*attributesConfig{},
	}
}

// Generic config for single operation.
type operationConfig struct {
	matcher func(name string) bool
	cache   cache.Cache
}

// Operation-specific configs.
type iterConfig struct {
	operationConfig
	ttl   time.Duration
	codec IterCodec
}

type existsConfig struct {
	operationConfig
	existsTTL      time.Duration
	doesntExistTTL time.Duration
}

type getConfig struct {
	existsConfig
	contentTTL       time.Duration
	maxCacheableSize int
}

type getRangeConfig struct {
	operationConfig
	subrangeSize   int64
	maxSubRequests int
	attributesTTL  time.Duration
	subrangeTTL    time.Duration
}

type attributesConfig struct {
	operationConfig
	ttl time.Duration
}

func newOperationConfig(cache cache.Cache, matcher func(string) bool) operationConfig {
	if cache == nil {
		panic("cache")
	}
	if matcher == nil {
		panic("matcher")
	}

	return operationConfig{
		matcher: matcher,
		cache:   cache,
	}
}

// CacheIter configures caching of "Iter" operation for matching directories.
func (cfg *CachingBucketConfig) CacheIter(configName string, cache cache.Cache, matcher func(string) bool, ttl time.Duration, codec IterCodec) {
	cfg.iter[configName] = &iterConfig{
		operationConfig: newOperationConfig(cache, matcher),
		ttl:             ttl,
		codec:           codec,
	}
}

// CacheGet configures caching of "Get" operation for matching files. Content of the object is cached, as well as whether object exists or not.
func (cfg *CachingBucketConfig) CacheGet(configName string, cache cache.Cache, matcher func(string) bool, maxCacheableSize int, contentTTL, existsTTL, doesntExistTTL time.Duration) {
	cfg.get[configName] = &getConfig{
		existsConfig: existsConfig{
			operationConfig: newOperationConfig(cache, matcher),
			existsTTL:       existsTTL,
			doesntExistTTL:  doesntExistTTL,
		},
		contentTTL:       contentTTL,
		maxCacheableSize: maxCacheableSize,
	}
}

// CacheExists configures caching of "Exists" operation for matching files. Negative values are cached as well.
func (cfg *CachingBucketConfig) CacheExists(configName string, cache cache.Cache, matcher func(string) bool, existsTTL, doesntExistTTL time.Duration) {
	cfg.exists[configName] = &existsConfig{
		operationConfig: newOperationConfig(cache, matcher),
		existsTTL:       existsTTL,
		doesntExistTTL:  doesntExistTTL,
	}
}

// CacheGetRange configures caching of "GetRange" operation. Subranges (aligned on subrange size) are cached individually.
// Since caching operation needs to know the object size to compute correct subranges, object size is cached as well.
// Single "GetRange" requests can result in multiple smaller GetRange sub-requests issued on the underlying bucket.
// MaxSubRequests specifies how many such subrequests may be issued. Values <= 0 mean there is no limit (requests
// for adjacent missing subranges are still merged).
func (cfg *CachingBucketConfig) CacheGetRange(configName string, cache cache.Cache, matcher func(string) bool, subrangeSize int64, attributesTTL, subrangeTTL time.Duration, maxSubRequests int) {
	cfg.getRange[configName] = &getRangeConfig{
		operationConfig: newOperationConfig(cache, matcher),
		subrangeSize:    subrangeSize,
		attributesTTL:   attributesTTL,
		subrangeTTL:     subrangeTTL,
		maxSubRequests:  maxSubRequests,
	}
}

// CacheAttributes configures caching of "Attributes" operation for matching files.
func (cfg *CachingBucketConfig) CacheAttributes(configName string, cache cache.Cache, matcher func(name string) bool, ttl time.Duration) {
	cfg.attributes[configName] = &attributesConfig{
		operationConfig: newOperationConfig(cache, matcher),
		ttl:             ttl,
	}
}

func (cfg *CachingBucketConfig) allConfigNames() map[string][]string {
	result := map[string][]string{}
	for n := range cfg.get {
		result[objstore.OpGet] = append(result[objstore.OpGet], n)
	}
	for n := range cfg.iter {
		result[objstore.OpIter] = append(result[objstore.OpIter], n)
	}
	for n := range cfg.exists {
		result[objstore.OpExists] = append(result[objstore.OpExists], n)
	}
	for n := range cfg.getRange {
		result[objstore.OpGetRange] = append(result[objstore.OpGetRange], n)
	}
	for n := range cfg.attributes {
		result[objstore.OpAttributes] = append(result[objstore.OpAttributes], n)
	}
	return result
}

func (cfg *CachingBucketConfig) findIterConfig(dir string) (string, *iterConfig) {
	for n, cfg := range cfg.iter {
		if cfg.matcher(dir) {
			return n, cfg
		}
	}
	return "", nil
}

func (cfg *CachingBucketConfig) findExistConfig(name string) (string, *existsConfig) {
	for n, cfg := range cfg.exists {
		if cfg.matcher(name) {
			return n, cfg
		}
	}
	return "", nil
}

func (cfg *CachingBucketConfig) findGetConfig(name string) (string, *getConfig) {
	for n, cfg := range cfg.get {
		if cfg.matcher(name) {
			return n, cfg
		}
	}
	return "", nil
}

func (cfg *CachingBucketConfig) findGetRangeConfig(name string) (string, *getRangeConfig) {
	for n, cfg := range cfg.getRange {
		if cfg.matcher(name) {
			return n, cfg
		}
	}
	return "", nil
}

func (cfg *CachingBucketConfig) findAttributesConfig(name string) (string, *attributesConfig) {
	for n, cfg := range cfg.attributes {
		if cfg.matcher(name) {
			return n, cfg
		}
	}
	return "", nil
}
