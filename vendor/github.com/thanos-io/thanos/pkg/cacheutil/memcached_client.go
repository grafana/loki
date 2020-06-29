// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package cacheutil

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/bradfitz/gomemcache/memcache"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"gopkg.in/yaml.v2"

	"github.com/thanos-io/thanos/pkg/discovery/dns"
	"github.com/thanos-io/thanos/pkg/extprom"
	"github.com/thanos-io/thanos/pkg/gate"
	"github.com/thanos-io/thanos/pkg/model"
)

const (
	opSet             = "set"
	opGetMulti        = "getmulti"
	reasonMaxItemSize = "max-item-size"
)

var (
	errMemcachedAsyncBufferFull = errors.New("the async buffer is full")
	errMemcachedConfigNoAddrs   = errors.New("no memcached addresses provided")

	defaultMemcachedClientConfig = MemcachedClientConfig{
		Timeout:                   500 * time.Millisecond,
		MaxIdleConnections:        100,
		MaxAsyncConcurrency:       20,
		MaxAsyncBufferSize:        10000,
		MaxItemSize:               model.Bytes(1024 * 1024),
		MaxGetMultiConcurrency:    100,
		MaxGetMultiBatchSize:      0,
		DNSProviderUpdateInterval: 10 * time.Second,
	}
)

// MemcachedClient is a high level client to interact with memcached.
type MemcachedClient interface {
	// GetMulti fetches multiple keys at once from memcached. In case of error,
	// an empty map is returned and the error tracked/logged.
	GetMulti(ctx context.Context, keys []string) map[string][]byte

	// SetAsync enqueues an asynchronous operation to store a key into memcached.
	// Returns an error in case it fails to enqueue the operation. In case the
	// underlying async operation will fail, the error will be tracked/logged.
	SetAsync(ctx context.Context, key string, value []byte, ttl time.Duration) error

	// Stop client and release underlying resources.
	Stop()
}

// memcachedClientBackend is an interface used to mock the underlying client in tests.
type memcachedClientBackend interface {
	GetMulti(keys []string) (map[string]*memcache.Item, error)
	Set(item *memcache.Item) error
}

// MemcachedClientConfig is the config accepted by MemcachedClient.
type MemcachedClientConfig struct {
	// Addresses specifies the list of memcached addresses. The addresses get
	// resolved with the DNS provider.
	Addresses []string `yaml:"addresses"`

	// Timeout specifies the socket read/write timeout.
	Timeout time.Duration `yaml:"timeout"`

	// MaxIdleConnections specifies the maximum number of idle connections that
	// will be maintained per address. For better performances, this should be
	// set to a number higher than your peak parallel requests.
	MaxIdleConnections int `yaml:"max_idle_connections"`

	// MaxAsyncConcurrency specifies the maximum number of concurrent asynchronous
	// operations can occur.
	MaxAsyncConcurrency int `yaml:"max_async_concurrency"`

	// MaxAsyncBufferSize specifies the maximum number of enqueued asynchronous
	// operations allowed.
	MaxAsyncBufferSize int `yaml:"max_async_buffer_size"`

	// MaxGetMultiConcurrency specifies the maximum number of concurrent connections
	// running GetMulti() operations. If set to 0, concurrency is unlimited.
	MaxGetMultiConcurrency int `yaml:"max_get_multi_concurrency"`

	// MaxItemSize specifies the maximum size of an item stored in memcached. Bigger
	// items are skipped to be stored by the client. If set to 0, no maximum size is
	// enforced.
	MaxItemSize model.Bytes `yaml:"max_item_size"`

	// MaxGetMultiBatchSize specifies the maximum number of keys a single underlying
	// GetMulti() should run. If more keys are specified, internally keys are splitted
	// into multiple batches and fetched concurrently, honoring MaxGetMultiConcurrency
	// parallelism. If set to 0, the max batch size is unlimited.
	MaxGetMultiBatchSize int `yaml:"max_get_multi_batch_size"`

	// DNSProviderUpdateInterval specifies the DNS discovery update interval.
	DNSProviderUpdateInterval time.Duration `yaml:"dns_provider_update_interval"`
}

func (c *MemcachedClientConfig) validate() error {
	if len(c.Addresses) == 0 {
		return errMemcachedConfigNoAddrs
	}

	return nil
}

// parseMemcachedClientConfig unmarshals a buffer into a MemcachedClientConfig with default values.
func parseMemcachedClientConfig(conf []byte) (MemcachedClientConfig, error) {
	config := defaultMemcachedClientConfig
	if err := yaml.Unmarshal(conf, &config); err != nil {
		return MemcachedClientConfig{}, err
	}

	return config, nil
}

type memcachedClient struct {
	logger   log.Logger
	config   MemcachedClientConfig
	client   memcachedClientBackend
	selector *MemcachedJumpHashSelector

	// DNS provider used to keep the memcached servers list updated.
	dnsProvider *dns.Provider

	// Channel used to notify internal goroutines when they should quit.
	stop chan struct{}

	// Channel used to enqueue async operations.
	asyncQueue chan func()

	// Gate used to enforce the max number of concurrent GetMulti() operations.
	getMultiGate gate.Gate

	// Wait group used to wait all workers on stopping.
	workers sync.WaitGroup

	// Tracked metrics.
	operations *prometheus.CounterVec
	failures   *prometheus.CounterVec
	skipped    *prometheus.CounterVec
	duration   *prometheus.HistogramVec
}

type memcachedGetMultiResult struct {
	items map[string]*memcache.Item
	err   error
}

// NewMemcachedClient makes a new MemcachedClient.
func NewMemcachedClient(logger log.Logger, name string, conf []byte, reg prometheus.Registerer) (*memcachedClient, error) {
	config, err := parseMemcachedClientConfig(conf)
	if err != nil {
		return nil, err
	}

	return NewMemcachedClientWithConfig(logger, name, config, reg)
}

// NewMemcachedClientWithConfig makes a new MemcachedClient.
func NewMemcachedClientWithConfig(logger log.Logger, name string, config MemcachedClientConfig, reg prometheus.Registerer) (*memcachedClient, error) {
	if err := config.validate(); err != nil {
		return nil, err
	}

	// We use a custom servers selector in order to use a jump hash
	// for servers selection.
	selector := &MemcachedJumpHashSelector{}

	client := memcache.NewFromSelector(selector)
	client.Timeout = config.Timeout
	client.MaxIdleConns = config.MaxIdleConnections

	if reg != nil {
		reg = prometheus.WrapRegistererWith(prometheus.Labels{"name": name}, reg)
	}
	return newMemcachedClient(logger, client, selector, config, reg)
}

func newMemcachedClient(
	logger log.Logger,
	client memcachedClientBackend,
	selector *MemcachedJumpHashSelector,
	config MemcachedClientConfig,
	reg prometheus.Registerer,
) (*memcachedClient, error) {
	dnsProvider := dns.NewProvider(
		logger,
		extprom.WrapRegistererWithPrefix("thanos_memcached_", reg),
		dns.GolangResolverType,
	)

	c := &memcachedClient{
		logger:      logger,
		config:      config,
		client:      client,
		selector:    selector,
		dnsProvider: dnsProvider,
		asyncQueue:  make(chan func(), config.MaxAsyncBufferSize),
		stop:        make(chan struct{}, 1),
		getMultiGate: gate.NewKeeper(extprom.WrapRegistererWithPrefix("thanos_memcached_getmulti_", reg)).
			NewGate(config.MaxGetMultiConcurrency),
	}

	c.operations = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thanos_memcached_operations_total",
		Help: "Total number of operations against memcached.",
	}, []string{"operation"})
	c.operations.WithLabelValues(opGetMulti)
	c.operations.WithLabelValues(opSet)

	c.failures = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thanos_memcached_operation_failures_total",
		Help: "Total number of operations against memcached that failed.",
	}, []string{"operation"})
	c.failures.WithLabelValues(opGetMulti)
	c.failures.WithLabelValues(opSet)

	c.skipped = promauto.With(reg).NewCounterVec(prometheus.CounterOpts{
		Name: "thanos_memcached_operation_skipped_total",
		Help: "Total number of operations against memcached that have been skipped.",
	}, []string{"operation", "reason"})
	c.skipped.WithLabelValues(opGetMulti, reasonMaxItemSize)
	c.skipped.WithLabelValues(opSet, reasonMaxItemSize)

	c.duration = promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{
		Name:    "thanos_memcached_operation_duration_seconds",
		Help:    "Duration of operations against memcached.",
		Buckets: []float64{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.2, 0.5, 1, 3, 6, 10},
	}, []string{"operation"})
	c.duration.WithLabelValues(opGetMulti)
	c.duration.WithLabelValues(opSet)

	// As soon as the client is created it must ensure that memcached server
	// addresses are resolved, so we're going to trigger an initial addresses
	// resolution here.
	if err := c.resolveAddrs(); err != nil {
		return nil, err
	}

	c.workers.Add(1)
	go c.resolveAddrsLoop()

	// Start a number of goroutines - processing async operations - equal
	// to the max concurrency we have.
	c.workers.Add(c.config.MaxAsyncConcurrency)
	for i := 0; i < c.config.MaxAsyncConcurrency; i++ {
		go c.asyncQueueProcessLoop()
	}

	return c, nil
}

func (c *memcachedClient) Stop() {
	close(c.stop)

	// Wait until all workers have terminated.
	c.workers.Wait()
}

func (c *memcachedClient) SetAsync(_ context.Context, key string, value []byte, ttl time.Duration) error {
	// Skip hitting memcached at all if the item is bigger than the max allowed size.
	if c.config.MaxItemSize > 0 && uint64(len(value)) > uint64(c.config.MaxItemSize) {
		c.skipped.WithLabelValues(opSet, reasonMaxItemSize).Inc()
		return nil
	}

	return c.enqueueAsync(func() {
		start := time.Now()
		c.operations.WithLabelValues(opSet).Inc()

		err := c.client.Set(&memcache.Item{
			Key:        key,
			Value:      value,
			Expiration: int32(time.Now().Add(ttl).Unix()),
		})
		if err != nil {
			c.failures.WithLabelValues(opSet).Inc()

			// If the PickServer will fail for any reason the server address will be nil
			// and so missing in the logs. We're OK with that (it's a best effort).
			serverAddr, _ := c.selector.PickServer(key)
			level.Warn(c.logger).Log("msg", "failed to store item to memcached", "key", key, "sizeBytes", len(value), "server", serverAddr, "err", err)
			return
		}

		c.duration.WithLabelValues(opSet).Observe(time.Since(start).Seconds())
	})
}

func (c *memcachedClient) GetMulti(ctx context.Context, keys []string) map[string][]byte {
	if len(keys) == 0 {
		return nil
	}

	batches, err := c.getMultiBatched(ctx, keys)
	if err != nil {
		level.Warn(c.logger).Log("msg", "failed to fetch items from memcached", "numKeys", len(keys), "firstKey", keys[0], "err", err)

		// In case we have both results and an error, it means some batch requests
		// failed and other succeeded. In this case we prefer to log it and move on,
		// given returning some results from the cache is better than returning
		// nothing.
		if len(batches) == 0 {
			return nil
		}
	}

	hits := map[string][]byte{}
	for _, items := range batches {
		for key, item := range items {
			hits[key] = item.Value
		}
	}

	return hits
}

func (c *memcachedClient) getMultiBatched(ctx context.Context, keys []string) ([]map[string]*memcache.Item, error) {
	// Do not batch if the input keys are less than the max batch size.
	if (c.config.MaxGetMultiBatchSize <= 0) || (len(keys) <= c.config.MaxGetMultiBatchSize) {
		items, err := c.getMultiSingle(ctx, keys)
		if err != nil {
			return nil, err
		}

		return []map[string]*memcache.Item{items}, nil
	}

	// Calculate the number of expected results.
	batchSize := c.config.MaxGetMultiBatchSize
	numResults := len(keys) / batchSize
	if len(keys)%batchSize != 0 {
		numResults++
	}

	// Spawn a goroutine for each batch request. The max concurrency will be
	// enforced by getMultiSingle().
	results := make(chan *memcachedGetMultiResult, numResults)
	defer close(results)

	for batchStart := 0; batchStart < len(keys); batchStart += batchSize {
		batchEnd := batchStart + batchSize
		if batchEnd > len(keys) {
			batchEnd = len(keys)
		}

		batchKeys := keys[batchStart:batchEnd]

		c.workers.Add(1)
		go func() {
			defer c.workers.Done()

			res := &memcachedGetMultiResult{}
			res.items, res.err = c.getMultiSingle(ctx, batchKeys)

			results <- res
		}()
	}

	// Wait for all batch results. In case of error, we keep
	// track of the last error occurred.
	items := make([]map[string]*memcache.Item, 0, numResults)
	var lastErr error

	for i := 0; i < numResults; i++ {
		result := <-results
		if result.err != nil {
			lastErr = result.err
			continue
		}

		items = append(items, result.items)
	}

	return items, lastErr
}

func (c *memcachedClient) getMultiSingle(ctx context.Context, keys []string) (items map[string]*memcache.Item, err error) {
	// Wait until we get a free slot from the gate, if the max
	// concurrency should be enforced.
	if c.config.MaxGetMultiConcurrency > 0 {
		if err := c.getMultiGate.Start(ctx); err != nil {
			return nil, errors.Wrapf(err, "failed to wait for turn")
		}
		defer c.getMultiGate.Done()
	}

	start := time.Now()
	c.operations.WithLabelValues(opGetMulti).Inc()
	items, err = c.client.GetMulti(keys)
	if err != nil {
		c.failures.WithLabelValues(opGetMulti).Inc()
	} else {
		c.duration.WithLabelValues(opGetMulti).Observe(time.Since(start).Seconds())
	}

	return items, err
}

func (c *memcachedClient) enqueueAsync(op func()) error {
	select {
	case c.asyncQueue <- op:
		return nil
	default:
		return errMemcachedAsyncBufferFull
	}
}

func (c *memcachedClient) asyncQueueProcessLoop() {
	defer c.workers.Done()

	for {
		select {
		case op := <-c.asyncQueue:
			op()
		case <-c.stop:
			return
		}
	}
}

func (c *memcachedClient) resolveAddrsLoop() {
	defer c.workers.Done()

	ticker := time.NewTicker(c.config.DNSProviderUpdateInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			err := c.resolveAddrs()
			if err != nil {
				level.Warn(c.logger).Log("msg", "failed update memcached servers list", "err", err)
			}
		case <-c.stop:
			return
		}
	}
}

func (c *memcachedClient) resolveAddrs() error {
	// Resolve configured addresses with a reasonable timeout.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// If some of the dns resolution fails, log the error.
	if err := c.dnsProvider.Resolve(ctx, c.config.Addresses); err != nil {
		level.Error(c.logger).Log("msg", "failed to resolve addresses for storeAPIs", "addresses", strings.Join(c.config.Addresses, ","), "err", err)
	}
	// Fail in case no server address is resolved.
	servers := c.dnsProvider.Addresses()
	if len(servers) == 0 {
		return errors.New("no server address resolved")
	}

	return c.selector.SetServers(servers...)
}
