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
	"github.com/prometheus/tsdb/fileutil"
	"golang.org/x/sys/unix"

	"github.com/weaveworks/cortex/pkg/util"
)

// TODO: in the future we could cuckoo hash or linear probe.

// Buckets contain key (~50), chunks (1024) and their metadata (~100)
const bucketSize = 2048

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
	mtx     sync.RWMutex
	f       *os.File
	buckets uint32
	buf     []byte
}

// NewDiskcache creates a new on-disk cache.
func NewDiskcache(cfg DiskcacheConfig) (*Diskcache, error) {
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

	buckets := len(buf) / bucketSize

	return &Diskcache{
		f:       f,
		buf:     buf,
		buckets: uint32(buckets),
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
	d.mtx.RLock()
	defer d.mtx.RUnlock()

	bucket := hash(key) % d.buckets
	buf := d.buf[bucket*bucketSize : (bucket+1)*bucketSize]

	existingKey, n, ok := get(buf, 0)
	if !ok || string(existingKey) != key {
		return nil, false
	}

	existingValue, _, ok := get(buf, n)
	if !ok {
		return nil, false
	}

	result := make([]byte, len(existingValue), len(existingValue))
	copy(result, existingValue)
	return result, true
}

// Store puts a chunk into the cache.
func (d *Diskcache) Store(ctx context.Context, keys []string, bufs [][]byte) {
	sp := opentracing.SpanFromContext(ctx)

	d.mtx.Lock()
	defer d.mtx.Unlock()

	for i := range keys {
		bucket := hash(keys[i]) % d.buckets
		buf := d.buf[bucket*bucketSize : (bucket+1)*bucketSize]

		n, err := put([]byte(keys[i]), buf, 0)
		if err != nil {
			sp.LogFields(otlog.Error(err))
			level.Error(util.Logger).Log("msg", "failed to put key to diskcache", "err", err)
			continue
		}

		_, err = put(bufs[i], buf, n)
		if err != nil {
			sp.LogFields(otlog.Error(err))
			level.Error(util.Logger).Log("msg", "failed to put value to diskcache", "err", err)
			continue
		}
	}
}

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
