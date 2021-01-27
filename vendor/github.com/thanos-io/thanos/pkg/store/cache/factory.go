// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package storecache

import (
	"fmt"
	"strings"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/thanos/pkg/cacheutil"
	"gopkg.in/yaml.v2"
)

type IndexCacheProvider string

const (
	INMEMORY  IndexCacheProvider = "IN-MEMORY"
	MEMCACHED IndexCacheProvider = "MEMCACHED"
)

// IndexCacheConfig specifies the index cache config.
type IndexCacheConfig struct {
	Type   IndexCacheProvider `yaml:"type"`
	Config interface{}        `yaml:"config"`
}

// NewIndexCache initializes and returns new index cache.
func NewIndexCache(logger log.Logger, confContentYaml []byte, reg prometheus.Registerer) (IndexCache, error) {
	level.Info(logger).Log("msg", "loading index cache configuration")
	cacheConfig := &IndexCacheConfig{}
	if err := yaml.UnmarshalStrict(confContentYaml, cacheConfig); err != nil {
		return nil, errors.Wrap(err, "parsing config YAML file")
	}

	backendConfig, err := yaml.Marshal(cacheConfig.Config)
	if err != nil {
		return nil, errors.Wrap(err, "marshal content of cache backend configuration")
	}

	var cache IndexCache
	switch strings.ToUpper(string(cacheConfig.Type)) {
	case string(INMEMORY):
		cache, err = NewInMemoryIndexCache(logger, reg, backendConfig)
	case string(MEMCACHED):
		var memcached cacheutil.MemcachedClient
		memcached, err = cacheutil.NewMemcachedClient(logger, "index-cache", backendConfig, reg)
		if err == nil {
			cache, err = NewMemcachedIndexCache(logger, memcached, reg)
		}
	default:
		return nil, errors.Errorf("index cache with type %s is not supported", cacheConfig.Type)
	}
	if err != nil {
		return nil, errors.Wrap(err, fmt.Sprintf("create %s index cache", cacheConfig.Type))
	}
	return cache, nil
}
