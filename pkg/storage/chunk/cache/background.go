package cache

import (
	"context"
	"flag"
	"sync"

	"github.com/go-kit/log/level"
	opentracing "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/grafana/loki/pkg/util/flagext"
	util_log "github.com/grafana/loki/pkg/util/log"
)

// BackgroundConfig is config for a Background Cache.
type BackgroundConfig struct {
	WriteBackGoroutines      int              `yaml:"writeback_goroutines"`
	WriteBackBuffer          int              `yaml:"writeback_buffer"`
	WriteBackBufferSizeLimit flagext.ByteSize `yaml:"writeback_buffer_size_limit"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *BackgroundConfig) RegisterFlagsWithPrefix(prefix string, description string, f *flag.FlagSet) {
	f.IntVar(&cfg.WriteBackGoroutines, prefix+"background.write-back-concurrency", 10, description+"At what concurrency to write back to cache.")
	f.IntVar(&cfg.WriteBackBuffer, prefix+"background.write-back-buffer", 10000, description+"How many key batches to buffer for background write-back.")
	cfg.WriteBackBufferSizeLimit.Set("1GB")
	f.Var(&cfg.WriteBackBufferSizeLimit, prefix+"background.write-back-buffer-size-limit", description+"Size limit of buffer for background write-back.")
}

type backgroundCache struct {
	Cache

	wg        sync.WaitGroup
	quit      chan struct{}
	bgWrites  chan backgroundWrite
	name      string
	size      int
	sizeLimit int

	droppedWriteBack      prometheus.Counter
	droppedWriteBackBytes prometheus.Counter
	queueLength           prometheus.Gauge
	queueBytes            prometheus.Gauge
}

type backgroundWrite struct {
	keys []string
	bufs [][]byte
}

func (b *backgroundWrite) size() int {
	var sz int

	for _, buf := range b.bufs {
		sz += len(buf)
	}

	return sz
}

// NewBackground returns a new Cache that does stores on background goroutines.
func NewBackground(name string, cfg BackgroundConfig, cache Cache, reg prometheus.Registerer) Cache {
	c := &backgroundCache{
		Cache:     cache,
		quit:      make(chan struct{}),
		bgWrites:  make(chan backgroundWrite, cfg.WriteBackBuffer),
		name:      name,
		sizeLimit: cfg.WriteBackBufferSizeLimit.Val(),

		droppedWriteBack: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   "loki",
			Name:        "cache_dropped_background_writes_total",
			Help:        "Total count of dropped write backs to cache.",
			ConstLabels: prometheus.Labels{"name": name},
		}),
		droppedWriteBackBytes: promauto.With(reg).NewCounter(prometheus.CounterOpts{
			Namespace:   "loki",
			Name:        "cache_dropped_background_writes_bytes_total",
			Help:        "Volume of chunk data dropped in write backs to cache.",
			ConstLabels: prometheus.Labels{"name": name},
		}),

		queueLength: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace:   "loki",
			Name:        "cache_background_queue_length",
			Help:        "Length of the cache background write queue.",
			ConstLabels: prometheus.Labels{"name": name},
		}),

		queueBytes: promauto.With(reg).NewGauge(prometheus.GaugeOpts{
			Namespace:   "loki",
			Name:        "cache_background_queue_bytes",
			Help:        "Volume of chunk bytes of the cache background write queue.",
			ConstLabels: prometheus.Labels{"name": name},
		}),
	}

	c.wg.Add(cfg.WriteBackGoroutines)
	for i := 0; i < cfg.WriteBackGoroutines; i++ {
		go c.writeBackLoop()
	}

	return c
}

// Stop the background flushing goroutines.
func (c *backgroundCache) Stop() {
	close(c.quit)
	c.wg.Wait()

	c.Cache.Stop()
}

const keysPerBatch = 100

// Store writes keys for the cache in the background.
func (c *backgroundCache) Store(ctx context.Context, keys []string, bufs [][]byte) error {
	for len(keys) > 0 {
		num := keysPerBatch
		if num > len(keys) {
			num = len(keys)
		}

		bgWrite := backgroundWrite{
			keys: keys[:num],
			bufs: bufs[:num],
		}

		size := bgWrite.size()
		newSize := c.size + size
		if newSize > c.sizeLimit {
			c.failStore(ctx, size, num, "queue at byte size limit")
			return nil
		}

		select {
		case c.bgWrites <- bgWrite:
			c.size = newSize
			c.queueBytes.Set(float64(c.size))
			c.queueLength.Add(float64(num))
		default:
			c.failStore(ctx, size, num, "queue at full capacity")
			return nil // queue is full; give up
		}
		keys = keys[num:]
		bufs = bufs[num:]
	}
	return nil
}

func (c *backgroundCache) failStore(ctx context.Context, size int, num int, reason string) {
	c.droppedWriteBackBytes.Add(float64(size))
	c.droppedWriteBack.Add(float64(num))
	sp := opentracing.SpanFromContext(ctx)
	if sp != nil {
		sp.LogFields(otlog.String("reason", reason), otlog.Int("dropped", num), otlog.Int("dropped_bytes", size))
	}
}

func (c *backgroundCache) writeBackLoop() {
	defer c.wg.Done()

	for {
		select {
		case bgWrite, ok := <-c.bgWrites:
			if !ok {
				return
			}
			c.size -= bgWrite.size()

			c.queueLength.Sub(float64(len(bgWrite.keys)))
			c.queueBytes.Set(float64(c.size))
			err := c.Cache.Store(context.Background(), bgWrite.keys, bgWrite.bufs)
			if err != nil {
				level.Warn(util_log.Logger).Log("msg", "backgroundCache writeBackLoop Cache.Store fail", "err", err)
				continue
			}

		case <-c.quit:
			return
		}
	}
}
