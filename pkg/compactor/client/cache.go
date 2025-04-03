package client

import (
	"context"
	"fmt"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/gogo/protobuf/proto"
	"github.com/grafana/loki/v3/pkg/compactor/client/grpc"
	"github.com/grafana/loki/v3/pkg/compactor/deletion"
	"github.com/grafana/loki/v3/pkg/logqlmodel/stats"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/constants"
	"github.com/opentracing/opentracing-go"
	"github.com/prometheus/client_golang/prometheus"
	"time"
)

type CacheConfig struct {
	CacheConfig  cache.Config `yaml:"cache"`
	CacheResults bool         `yaml:"cache_results"`
}

type CacheGenNumberLoader interface {
	GetResultsCacheGenNumber(tenantIDs []string) string
}

type Cache struct {
	deletion.CompactorClient
	c              cache.Cache
	cacheGenLoader CacheGenNumberLoader
	logger         log.Logger
}

func NewCacheMiddleware(client deletion.CompactorClient, cfg CacheConfig, cacheGenLoader CacheGenNumberLoader, reg prometheus.Registerer, log log.Logger) (*Cache, error) {
	if !cfg.CacheResults {
		return nil, fmt.Errorf("cache compactor results must be enabled")
	}

	c, err := cache.New(cfg.CacheConfig, reg, log, stats.DeletesCache, constants.Loki)
	if err != nil {
		return nil, err
	}

	return &Cache{
		CompactorClient: client,
		c:               c,
		cacheGenLoader:  cacheGenLoader,
		logger:          log,
	}, nil
}

func (c *Cache) GetAllDeleteRequestsForUser(ctx context.Context, userID string) ([]deletion.DeleteRequest, error) {
	sp, ctx := opentracing.StartSpanFromContext(ctx, "resultsCache.Do")
	defer sp.Finish()

	var cacheGen string
	if c.cacheGenLoader != nil {
		cacheGen = c.cacheGenLoader.GetResultsCacheGenNumber([]string{userID})
	}

	key := c.key(userID)
	sp.LogKV("key", key, "cache_gen", cacheGen)

	cached, cachedAt, ok := c.get(ctx, key, cacheGen)
	if ok {
		sp.LogKV("cached", true, "cached_at", cachedAt.String(), "deletes", len(cached))
		return cached, nil
	}

	// Handle miss
	deletes, err := c.CompactorClient.GetAllDeleteRequestsForUser(ctx, userID)
	if err != nil {
		return nil, err
	}

	sp.LogKV("cached", false, "deletes", len(deletes))
	if err = c.put(ctx, key, deletes, cacheGen); err != nil {
		// We only log the error here, as we don't want to return an error to the caller if the cache write fails.
		sp.LogKV("msg", "error caching delete requests", "err", err)
		return deletes, nil
	}

	return deletes, nil
}

func (c *Cache) key(userID string) string {
	return fmt.Sprintf("delete_requests_%s", userID)
}

func (c *Cache) get(ctx context.Context, key string, cacheGen string) ([]deletion.DeleteRequest, time.Time, bool) {
	found, bufs, _, _ := c.c.Fetch(ctx, []string{cache.HashKey(key)})
	if len(found) != 1 {
		return nil, time.Time{}, false
	}

	var cached grpc.CachedDeletes
	if err := proto.Unmarshal(bufs[0], &cached); err != nil {
		level.Error(c.logger).Log("msg", "error unmarshalling cached value", "err", err)
		return nil, time.Time{}, false
	}

	if cached.Key != key {
		level.Error(c.logger).Log("msg", "cached key does not match requested key", "cached_key", cached.Key, "requested_key", key)
		return nil, time.Time{}, false
	}

	if cacheGen != "" && cached.CacheGen != cacheGen {
		level.Debug(c.logger).Log("msg", "cache gen mismatch", "cached_cache_gen", cached.CacheGen, "current_cache_gen", cacheGen)
		return nil, time.Time{}, false
	}

	cachedTime := time.UnixMilli(cached.AddedAtTS)
	return grpc.DeleteRequestsFromProto(cached.DeleteRequests), cachedTime, true
}

func (c *Cache) put(ctx context.Context, key string, deletes []deletion.DeleteRequest, cacheGen string) error {
	buf, err := proto.Marshal(&grpc.CachedDeletes{
		Key:            key,
		AddedAtTS:      time.Now().UnixMilli(),
		CacheGen:       cacheGen,
		DeleteRequests: grpc.DeleteRequestsToProto(deletes),
	})
	if err != nil {
		return fmt.Errorf("error marshalling cached value: %w", err)
	}

	return c.c.Store(ctx, []string{cache.HashKey(key)}, [][]byte{buf})
}

func (c *Cache) Stop() {
	c.CompactorClient.Stop()
	c.c.Stop()
}
