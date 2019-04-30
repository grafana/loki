package cache

import (
	"context"
	"flag"
	"sync"

	opentracing "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	droppedWriteBack = promauto.NewCounterVec(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "cache_dropped_background_writes_total",
		Help:      "Total count of dropped write backs to cache.",
	}, []string{"name"})
	queueLength = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "cache_background_queue_length",
		Help:      "Length of the cache background write queue.",
	}, []string{"name"})
)

// BackgroundConfig is config for a Background Cache.
type BackgroundConfig struct {
	WriteBackGoroutines int `yaml:"writeback_goroutines,omitempty"`
	WriteBackBuffer     int `yaml:"writeback_buffer,omitempty"`
}

// RegisterFlagsWithPrefix adds the flags required to config this to the given FlagSet
func (cfg *BackgroundConfig) RegisterFlagsWithPrefix(prefix string, description string, f *flag.FlagSet) {
	f.IntVar(&cfg.WriteBackGoroutines, prefix+"memcache.write-back-goroutines", 10, description+"How many goroutines to use to write back to memcache.")
	f.IntVar(&cfg.WriteBackBuffer, prefix+"memcache.write-back-buffer", 10000, description+"How many chunks to buffer for background write back.")
}

type backgroundCache struct {
	Cache

	wg       sync.WaitGroup
	quit     chan struct{}
	bgWrites chan backgroundWrite
	name     string

	droppedWriteBack prometheus.Counter
	queueLength      prometheus.Gauge
}

type backgroundWrite struct {
	keys []string
	bufs [][]byte
}

// NewBackground returns a new Cache that does stores on background goroutines.
func NewBackground(name string, cfg BackgroundConfig, cache Cache) Cache {
	c := &backgroundCache{
		Cache:            cache,
		quit:             make(chan struct{}),
		bgWrites:         make(chan backgroundWrite, cfg.WriteBackBuffer),
		name:             name,
		droppedWriteBack: droppedWriteBack.WithLabelValues(name),
		queueLength:      queueLength.WithLabelValues(name),
	}

	c.wg.Add(cfg.WriteBackGoroutines)
	for i := 0; i < cfg.WriteBackGoroutines; i++ {
		go c.writeBackLoop()
	}

	return c
}

// Stop the background flushing goroutines.
func (c *backgroundCache) Stop() error {
	close(c.quit)
	c.wg.Wait()

	return c.Cache.Stop()
}

// Store writes keys for the cache in the background.
func (c *backgroundCache) Store(ctx context.Context, keys []string, bufs [][]byte) {
	bgWrite := backgroundWrite{
		keys: keys,
		bufs: bufs,
	}
	select {
	case c.bgWrites <- bgWrite:
		c.queueLength.Add(float64(len(keys)))
	default:
		c.droppedWriteBack.Add(float64(len(keys)))
		sp := opentracing.SpanFromContext(ctx)
		if sp != nil {
			sp.LogFields(otlog.Int("dropped", len(keys)))
		}
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
			c.queueLength.Sub(float64(len(bgWrite.keys)))
			c.Cache.Store(context.Background(), bgWrite.keys, bgWrite.bufs)

		case <-c.quit:
			return
		}
	}
}
