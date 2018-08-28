package cache

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"hash/fnv"
	"os"
	"sync"

	"github.com/go-kit/kit/log/level"
	opentracing "github.com/opentracing/opentracing-go"
	otlog "github.com/opentracing/opentracing-go/log"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/tsdb/fileutil"
	"golang.org/x/sys/unix"

	"github.com/cortexproject/cortex/pkg/util"
)

var (
	bucketsTotal = promauto.NewGauge(prometheus.GaugeOpts{
		Namespace: "cortex",
		Name:      "diskcache_buckets_total",
		Help:      "Total count of buckets in the cache.",
	})
	bucketsInitialized = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "diskcache_added_new_total",
		Help:      "total number of entries added to the cache",
	})
	collisionsTotal = promauto.NewCounter(prometheus.CounterOpts{
		Namespace: "cortex",
		Name:      "diskcache_evicted_total",
		Help:      "total number entries evicted from the cache",
	})

	globalCache *Diskcache
	once        sync.Once
)

// TODO: in the future we could cuckoo hash or linear probe.

const (
	// Buckets contain chunks (1024) and their metadata (~100)
	bucketSize = 2048

	// Total number of mutexes shared by the disk cache index
	numMutexes = 1000
)

// DiskcacheConfig for the Disk cache.
type DiskcacheConfig struct {
	Path string
	Size int
}

// RegisterFlags adds the flags required to config this to the given FlagSet
func (cfg *DiskcacheConfig) RegisterFlags(f *flag.FlagSet) {
	f.StringVar(&cfg.Path, "diskcache.path", "/var/run/chunks", "Path to file used to cache chunks.")
	f.IntVar(&cfg.Size, "diskcache.size", 1024*1024*1024, "Size of file (bytes)")
}

// Diskcache is an on-disk chunk cache.
type Diskcache struct {
	f            *os.File
	buckets      uint32
	buf          []byte
	entries      []string
	entryMutexes []sync.RWMutex
}

// NewDiskcache creates a new on-disk cache.
func NewDiskcache(cfg DiskcacheConfig) (*Diskcache, error) {
	var err error
	once.Do(func() {
		globalCache, err = newDiskcache(cfg)
	})
	return globalCache, err
}

func newDiskcache(cfg DiskcacheConfig) (*Diskcache, error) {
	f, err := os.OpenFile(cfg.Path, os.O_RDWR|os.O_CREATE, 0644)
	if err != nil {
		return nil, errors.Wrap(err, "open")
	}

	if err := fileutil.Preallocate(f, int64(cfg.Size), true); err != nil {
		return nil, errors.Wrap(err, "preallocate")
	}

	info, err := f.Stat()
	if err != nil {
		return nil, errors.Wrap(err, "stat")
	}

	buf, err := unix.Mmap(int(f.Fd()), 0, int(info.Size()), unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		f.Close()
		return nil, err
	}

	buckets := uint32(len(buf) / bucketSize)
	bucketsTotal.Set(float64(buckets)) // Report the number of buckets in the diskcache as a metric

	return &Diskcache{
		f:            f,
		buckets:      buckets,
		buf:          buf,
		entries:      make([]string, buckets),
		entryMutexes: make([]sync.RWMutex, numMutexes),
	}, nil
}

// Stop closes the file.
func (d *Diskcache) Stop() error {
	if err := unix.Munmap(d.buf); err != nil {
		return err
	}
	return d.f.Close()
}

// Fetch get chunks from the cache.
func (d *Diskcache) Fetch(ctx context.Context, keys []string) (found []string, bufs [][]byte, missed []string) {
	for _, key := range keys {
		buf, ok := d.fetch(key)
		if ok {
			found = append(found, key)
			bufs = append(bufs, buf)
		} else {
			missed = append(missed, key)
		}
	}
	return
}

func (d *Diskcache) fetch(key string) ([]byte, bool) {
	bucket := hash(key) % d.buckets
	shard := bucket % numMutexes // Get the index of the mutex associated with this bucket
	d.entryMutexes[shard].RLock()
	defer d.entryMutexes[shard].RUnlock()
	if d.entries[bucket] != key {
		return nil, false
	}

	buf := d.buf[bucket*bucketSize : (bucket+1)*bucketSize]
	existingValue, _, ok := get(buf, 0)
	if !ok {
		return nil, false
	}

	result := make([]byte, len(existingValue), len(existingValue))
	copy(result, existingValue)
	return result, true
}

// Store puts a chunk into the cache.
func (d *Diskcache) Store(ctx context.Context, keys []string, bufs [][]byte) {
	for i := range keys {
		d.store(ctx, keys[i], bufs[i])
	}
}

func (d *Diskcache) store(ctx context.Context, key string, value []byte) {
	sp := opentracing.SpanFromContext(ctx)

	bucket := hash(key) % d.buckets
	shard := bucket % numMutexes // Get the index of the mutex associated with this bucket
	d.entryMutexes[shard].Lock()
	defer d.entryMutexes[shard].Unlock()
	if d.entries[bucket] == key { // If chunk is already cached return nil
		return
	}

	if d.entries[bucket] == "" {
		bucketsInitialized.Inc()
	} else {
		collisionsTotal.Inc()
	}

	buf := d.buf[bucket*bucketSize : (bucket+1)*bucketSize]
	_, err := put(value, buf, 0)
	if err != nil {
		d.entries[bucket] = ""
		sp.LogFields(otlog.Error(err))
		level.Error(util.Logger).Log("msg", "failed to put key to diskcache", "err", err)
		return
	}

	d.entries[bucket] = key
}

// put places a value in the buffer in the following format
// |u int64 <length of key> | key | uint64 <length of value> | value |
func put(value []byte, buf []byte, n int) (int, error) {
	if len(value)+n+4 > len(buf) {
		return 0, errors.Wrap(fmt.Errorf("value too big: %d > %d", len(value), len(buf)), "put")
	}
	m := binary.PutUvarint(buf[n:], uint64(len(value)))
	copy(buf[n+m:], value)
	return len(value) + n + m, nil
}

func get(buf []byte, n int) ([]byte, int, bool) {
	size, m := binary.Uvarint(buf[n:])
	end := n + m + int(size)
	if end > len(buf) {
		return nil, 0, false
	}
	return buf[n+m : end], end, true
}

func hash(key string) uint32 {
	h := fnv.New32()
	h.Write([]byte(key))
	return h.Sum32()
}
