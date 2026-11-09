package bucketcache

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"

	lru "github.com/hashicorp/golang-lru/v2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj/multirange"
	"github.com/grafana/loki/v3/pkg/storage/chunk/cache"
	"github.com/grafana/loki/v3/pkg/util/pool"
	"github.com/grafana/loki/v3/pkg/util/rangeio"
)

const (
	// The data type in a key. They are short to keep the keys short. A change to the layout
	// of a value needs a new data type, so the cache does not read the old entries.
	dataTypeSubrange   = "r"
	dataTypeAttributes = "a"

	// slabSubranges is how many subranges fit in one slab of the hit memory pool.
	slabSubranges = 10

	// fullKeyBudget is the room that a slab keeps for the full key at the start of each value. A longer
	// key still works, and then fewer subranges fit in a slab.
	fullKeyBudget = 256

	// valueOverhead is what memcached asks the allocator for on top of a value: the line ending.
	valueOverhead = 2

	// attributesLength is the length of the encoded attributes: the size and the modification time,
	// 8 bytes each.
	attributesLength = 16
)

// AttributesCache caches the attributes of an object. GetOrLoad must share one load between
// concurrent callers of one key, and load again a value that accept rejects. The slice it returns
// is read-only.
type AttributesCache interface {
	GetOrLoad(ctx context.Context, key string, accept func([]byte) bool, load func(context.Context) ([]byte, error)) ([]byte, error)
}

// cachedBucket serves GetRange, MultiGetRange and Attributes from the cache. It passes every other
// call to the bucket it wraps.
//
// A cache key holds a hash of the object name. Every cached value therefore starts with its full
// key, the key built from the full name. A read rejects a value with another full key, so two names
// that share a hash never serve each other's data.
type cachedBucket struct {
	multirange.Bucket

	bucketID        string
	subrangesCache  cache.Cache
	attributesCache AttributesCache
	attributesLRU   *lru.Cache[string, objstore.ObjectAttributes]
	subrangeSize    int64
	slabs           *sync.Pool
	metrics         *metrics
}

// newCachedBucket returns a cachedBucket that caches the reads of one kind of object in backend. bucketID
// scopes the keys to one bucket. The cachedBucket does not stop the caches.
func newCachedBucket(bucket multirange.Bucket, backend *Backend, kind, bucketID string, reg prometheus.Registerer) (*cachedBucket, error) {
	if bucket == nil || backend == nil || backend.Subranges == nil || backend.Attributes == nil {
		return nil, errors.New("the bucket cache needs a bucket, a subrange cache and an attributes cache")
	}
	cfg := backend.Config
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	b := &cachedBucket{
		Bucket:          bucket,
		bucketID:        bucketID,
		subrangesCache:  backend.Subranges,
		attributesCache: backend.Attributes,
		subrangeSize:    int64(cfg.SubrangeSize),
		slabs:           &sync.Pool{},
		metrics:         newMetrics(prometheus.WrapRegistererWith(prometheus.Labels{"kind": kind}, reg)),
	}
	if cfg.AttributesInMemoryMaxItems > 0 {
		var err error
		if b.attributesLRU, err = lru.New[string, objstore.ObjectAttributes](cfg.AttributesInMemoryMaxItems); err != nil {
			return nil, err
		}
	}
	return b, nil
}

// GetRange returns the range from the cache and the bucket. The reader holds memory from a pool
// until the caller closes it.
func (b *cachedBucket) GetRange(ctx context.Context, name string, offset, length int64) (io.ReadCloser, error) {
	// A length of -1 reads to the end of the object. The cache cannot split it, so the bucket serves it.
	if offset < 0 || length <= 0 {
		return b.Bucket.GetRange(ctx, name, offset, length)
	}

	attrs, err := b.objectAttributes(ctx, name)
	if err != nil {
		return nil, asUnexpectedEOF(err, fmt.Sprintf("reading attributes of %q", name))
	}

	size := attrs.Size
	if offset >= size {
		// The bucket answers, so a read past the end fails as it does without the cache.
		return b.Bucket.GetRange(ctx, name, offset, length)
	}

	length = min(length, size-offset)
	b.metrics.requestedBytes.Add(float64(length))

	var (
		first        = offset / b.subrangeSize * b.subrangeSize
		numSubranges = int((offset + length - first + b.subrangeSize - 1) / b.subrangeSize)
		subranges    = make([]subrange, numSubranges)
		keys         = make([]string, numSubranges)
		hashedName   = hashName(name)
	)

	for i := range subranges {
		start := first + int64(i)*b.subrangeSize
		subranges[i] = subrange{start: start, end: min(start+b.subrangeSize, size)}
		keys[i] = b.subrangeKey(hashedName, subranges[i])
	}

	slabs := pool.NewSlabPool(b.slabs, slabSubranges*(int(b.subrangeSize)+fullKeyBudget+valueOverhead))
	data := b.fetchSubrangesFromCache(ctx, slabs, name, keys, subranges)
	if err := b.fetchMissingSubrangesFromBucket(ctx, name, keys, subranges, data); err != nil {
		slabs.Release()
		return nil, err
	}

	return &reader{
		data:         data,
		subrangeSize: b.subrangeSize,
		pos:          offset - first,
		remaining:    length,
		slabs:        slabs,
	}, nil
}

// fetchSubrangesFromCache returns the cached data of each subrange. An entry is nil for a miss. A fetch error
// counts as a miss for the subranges that the fetch did not return.
func (b *cachedBucket) fetchSubrangesFromCache(ctx context.Context, slabs *pool.SlabPool, name string, keys []string, subranges []subrange) [][]byte {
	var (
		data     = make([][]byte, len(keys))
		hits     int
		hitBytes int64
	)

	// Fetch can return hits together with an error. A batched memcached returns the batches that
	// succeeded when another batch fails. Keep those hits, and count the rest as misses.
	foundKeys, bufs, _, err := b.subrangesCache.Fetch(ctx, keys, cache.WithAllocator(slabs.Allocator()))
	if err != nil && ctx.Err() == nil {
		b.metrics.errors.WithLabelValues(operationFetch).Inc()
	}

	keyToSubrangeIdx := make(map[string]int, len(keys))
	for i, key := range keys {
		keyToSubrangeIdx[key] = i
	}

	for foundIdx, foundKey := range foundKeys {
		subrangeIdx, ok := keyToSubrangeIdx[foundKey]
		if !ok {
			continue
		}

		// A value with another full key, or the wrong length, is a corrupted entry. A read of it would
		// fail or return the data of another object. foundKey holds the hash of the name, so the
		// full key comes from the name.
		expectedFullKey := b.subrangeKey(name, subranges[subrangeIdx])
		if int64(len(bufs[foundIdx])) != int64(len(expectedFullKey))+subranges[subrangeIdx].len() || string(bufs[foundIdx][:len(expectedFullKey)]) != expectedFullKey {
			b.metrics.corruptedEntries.Inc()
			continue
		}

		data[subrangeIdx] = bufs[foundIdx][len(expectedFullKey):]
		hits++
		hitBytes += subranges[subrangeIdx].len()
	}

	b.metrics.requestedSubranges.Add(float64(len(keys)))
	b.metrics.hitSubranges.Add(float64(hits))
	b.metrics.fetchedBytes.WithLabelValues(originCache).Add(float64(hitBytes))

	return data
}

// fetchMissingSubrangesFromBucket reads the missing subranges from the bucket into data, and stores them in the cache.
// It reads them with one call, so the bucket can merge the ones that are close.
func (b *cachedBucket) fetchMissingSubrangesFromBucket(ctx context.Context, name string, keys []string, subranges []subrange, data [][]byte) error {
	var (
		missing     []rangeio.Range // The buffer that each missing subrange is read into.
		missingIdx  []int
		storeKeys   []string
		storeBufs   [][]byte
		missedBytes int64
	)

	for i := range data {
		if data[i] != nil {
			// The subrange has been already fetched from the cache.
			continue
		}

		// Do not take this from the slab pool. The cache can keep the buffer after Store returns, and
		// Release reuses slab memory.
		fullKey := b.subrangeKey(name, subranges[i])
		value := make([]byte, len(fullKey)+int(subranges[i].len()))
		copy(value, fullKey)
		missing = append(missing, rangeio.Range{Data: value[len(fullKey):], Offset: subranges[i].start})
		missingIdx = append(missingIdx, i)
		storeKeys = append(storeKeys, keys[i])
		storeBufs = append(storeBufs, value)
		missedBytes += subranges[i].len()
	}

	if len(missing) == 0 {
		return nil
	}
	if err := b.Bucket.MultiGetRange(ctx, name, missing); err != nil {
		return asUnexpectedEOF(err, fmt.Sprintf("reading subranges of %q", name))
	}

	for j, i := range missingIdx {
		data[i] = missing[j].Data
	}

	b.metrics.fetchedBytes.WithLabelValues(originBucket).Add(float64(missedBytes))

	// Cache the subranges fetched from the bucket.
	if err := b.subrangesCache.Store(context.WithoutCancel(ctx), storeKeys, storeBufs); err != nil {
		b.metrics.errors.WithLabelValues(operationStore).Inc()
	}

	return nil
}

// MultiGetRange fills the ranges from the cache and the bucket. It looks up every block that the ranges need in one
// call, and reads the missing blocks from the bucket in one call. A block that two ranges share is looked up once.
func (b *cachedBucket) MultiGetRange(ctx context.Context, name string, ranges []rangeio.Range) error {
	var requested int64
	for _, r := range ranges {
		requested += r.Len()
	}
	if requested == 0 {
		return nil
	}

	attrs, err := b.objectAttributes(ctx, name)
	if err != nil {
		return asUnexpectedEOF(err, fmt.Sprintf("reading attributes of %q", name))
	}

	size := attrs.Size
	for _, r := range ranges {
		if r.Offset < 0 || r.Offset+r.Len() > size {
			// The bucket answers, so a read past the end fails as it does without the cache.
			return b.Bucket.MultiGetRange(ctx, name, ranges)
		}
	}
	b.metrics.requestedBytes.Add(float64(requested))

	// The blocks that the ranges need, in order, each once.
	var blocks []int64
	for _, r := range ranges {
		if len(r.Data) == 0 {
			continue
		}
		for block := r.Offset / b.subrangeSize; block*b.subrangeSize < r.Offset+r.Len(); block++ {
			blocks = append(blocks, block)
		}
	}
	slices.Sort(blocks)
	blocks = slices.Compact(blocks)

	var (
		subranges  = make([]subrange, len(blocks))
		keys       = make([]string, len(blocks))
		hashedName = hashName(name)
	)
	for i, block := range blocks {
		start := block * b.subrangeSize
		subranges[i] = subrange{start: start, end: min(start+b.subrangeSize, size)}
		keys[i] = b.subrangeKey(hashedName, subranges[i])
	}

	slabs := pool.NewSlabPool(b.slabs, slabSubranges*(int(b.subrangeSize)+fullKeyBudget+valueOverhead))
	defer slabs.Release()

	data := b.fetchSubrangesFromCache(ctx, slabs, name, keys, subranges)
	if err := b.fetchMissingSubrangesFromBucket(ctx, name, keys, subranges, data); err != nil {
		return err
	}

	for _, r := range ranges {
		end := r.Offset + r.Len()
		for pos := r.Offset; pos < end; {
			i, _ := slices.BinarySearch(blocks, pos/b.subrangeSize)
			pos += int64(copy(r.Data[pos-r.Offset:], data[i][pos-subranges[i].start:]))
		}
	}
	return nil
}

// Attributes returns the attributes of an object from the cache. The modification time keeps
// millisecond precision.
func (b *cachedBucket) Attributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	return b.objectAttributes(ctx, name)
}

// objectAttributes returns the attributes of an object from memory, then the attributes cache, then
// the bucket.
func (b *cachedBucket) objectAttributes(ctx context.Context, name string) (objstore.ObjectAttributes, error) {
	if b.attributesLRU != nil {
		if attrs, ok := b.attributesLRU.Get(name); ok {
			return attrs, nil
		}
	}

	fullKey := b.attributesKey(name)
	raw, err := b.attributesCache.GetOrLoad(ctx, b.attributesKey(hashName(name)),
		func(raw []byte) bool {
			_, ok := decodeAttributes(fullKey, raw)
			return ok
		},
		func(ctx context.Context) ([]byte, error) {
			attrs, err := b.Bucket.Attributes(ctx, name)
			if err != nil {
				return nil, err
			}
			raw := append(make([]byte, 0, len(fullKey)+attributesLength), fullKey...)
			raw = binary.BigEndian.AppendUint64(raw, uint64(attrs.Size))
			return binary.BigEndian.AppendUint64(raw, uint64(attrs.LastModified.UnixMilli())), nil
		})
	if err != nil {
		return objstore.ObjectAttributes{}, err
	}

	// A load that another name with the same hash started is shared, and returns the attributes of that name.
	attrs, ok := decodeAttributes(fullKey, raw)
	if !ok {
		return objstore.ObjectAttributes{}, fmt.Errorf("the cached attributes of %q belong to another object", name)
	}
	if b.attributesLRU != nil {
		b.attributesLRU.Add(name, attrs)
	}
	return attrs, nil
}

// BucketID returns an ID for the keys of the cache, from the name of the bucket and the prefix
// of the data objects in it.
func BucketID(bucketName, prefix string) string {
	sum := sha256.Sum256([]byte(bucketName + "/" + prefix))
	return base64.RawURLEncoding.EncodeToString(sum[:12])
}

// decodeAttributes returns the attributes in a value that starts with fullKey. It returns false for any
// other value.
func decodeAttributes(fullKey string, raw []byte) (objstore.ObjectAttributes, bool) {
	if len(raw) != len(fullKey)+attributesLength || string(raw[:len(fullKey)]) != fullKey {
		return objstore.ObjectAttributes{}, false
	}
	encoded := raw[len(fullKey):]
	return objstore.ObjectAttributes{
		Size:         int64(binary.BigEndian.Uint64(encoded[:8])),
		LastModified: time.UnixMilli(int64(binary.BigEndian.Uint64(encoded[8:]))).UTC(),
	}, true
}

// subrangeKey returns the cache key of a subrange. Pass hashName(name) for the key of the cache, and
// name for the full key.
func (b *cachedBucket) subrangeKey(name string, s subrange) string {
	return b.bucketID + ":" + dataTypeSubrange + ":" + name + ":" + strconv.FormatInt(s.start, 10) + ":" + strconv.FormatInt(s.end, 10)
}

// attributesKey returns the cache key of the attributes of an object. Pass hashName(name) for the key of
// the cache, and name for the full key.
func (b *cachedBucket) attributesKey(name string) string {
	return b.bucketID + ":" + dataTypeAttributes + ":" + name
}

// hashName returns a hash of the object name. It is shorter than most names, and keeps the key below
// the key length limit of the cache backends.
func hashName(name string) string {
	sum := sha256.Sum256([]byte(name))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// asUnexpectedEOF returns an io.ErrUnexpectedEOF error with err as its cause when err wraps io.EOF, and
// err otherwise.
func asUnexpectedEOF(err error, action string) error {
	if !errors.Is(err, io.EOF) {
		return err
	}
	return fmt.Errorf("%s: %w (cause: %v)", action, io.ErrUnexpectedEOF, err)
}

// subrange is the byte range [start, end) of an object.
type subrange struct{ start, end int64 }

func (s subrange) len() int64 { return s.end - s.start }

// reader reads the subranges of one GetRange. Close returns their memory to the pool.
type reader struct {
	data         [][]byte
	subrangeSize int64
	pos          int64 // Offset from the start of the first subrange.
	remaining    int64
	slabs        *pool.SlabPool
}

func (r *reader) Read(p []byte) (int, error) {
	if r.data == nil {
		return 0, os.ErrClosed
	}
	if r.remaining <= 0 {
		return 0, io.EOF
	}

	// Read from the subrange that pos points into, starting at pos. The last subrange of an object can
	// be shorter than subrangeSize.
	subrangeIdx := r.pos / r.subrangeSize
	offsetInSubrange := r.pos % r.subrangeSize
	if subrangeIdx >= int64(len(r.data)) || offsetInSubrange >= int64(len(r.data[subrangeIdx])) {
		// The reader holds less data than the range needs. Fail the read instead of panicking.
		return 0, io.ErrUnexpectedEOF
	}
	src := r.data[subrangeIdx][offsetInSubrange:]

	// Stop at the end of the requested range, which can end before the end of the subrange.
	if int64(len(src)) > r.remaining {
		src = src[:r.remaining]
	}
	n := copy(p, src)
	r.pos += int64(n)
	r.remaining -= int64(n)
	return n, nil
}

func (r *reader) Close() error {
	if r.data != nil {
		r.data = nil
		r.slabs.Release()
	}
	return nil
}
