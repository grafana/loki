package querier

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"sync"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// dataObjHeadPrefetchBytes is how many bytes FromBucket reads up front when opening an object. The
// encoder packs the file metadata and every section's metadata region contiguously at the head, so
// prefetching a window large enough to cover them serves each section's metadata and column descriptors
// from memory instead of a per-section round-trip; only the page data (in the later data regions) then
// needs a read. This trades a larger head read for fewer serial round-trips, which pays off at high
// object-storage latency.
const dataObjHeadPrefetchBytes = 256 * 1024

// dataObjCache opens each data object once per query and caches the opened object and its sections.
// The same object is read first for stream labels and then for log rows, so caching avoids re-reading
// the object header and section metadata.
//
// get is safe for concurrent use: object resolution opens objects from many goroutines. The returned
// openObject is shared — several logs-section reads of the same object run concurrently — so openObject
// guards its lazily-opened sections with its own lock.
type dataObjCache struct {
	bucket objstore.Bucket
	tenant string

	// metadataCache, when set, serves each object's metadata prefix so opening it does not read the
	// metadata from object storage. It is shared across queries; nil disables it.
	metadataCache dataobj.MetadataCache

	mu     sync.Mutex
	byPath map[string]*openObject
}

func newDataObjCache(bucket objstore.Bucket, tenant string) *dataObjCache {
	return &dataObjCache{
		bucket: bucket,
		tenant: tenant,
		byPath: map[string]*openObject{},
	}
}

func (c *dataObjCache) get(ctx context.Context, path string) (*openObject, error) {
	c.mu.Lock()
	oo, ok := c.byPath[path]
	c.mu.Unlock()
	if ok {
		return oo, nil
	}

	// Open outside the lock so concurrent opens of different objects don't serialize on the object
	// storage I/O.
	obj, err := dataobj.FromBucket(ctx, c.bucket, path, dataObjHeadPrefetchBytes, dataobj.WithMetadataCache(c.metadataCache))
	if err != nil {
		return nil, err
	}
	oo, err = c.resolveSections(obj)
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.byPath[path]; ok {
		// Another goroutine opened the same object while we did; keep the first one.
		return existing, nil
	}
	c.byPath[path] = oo
	return oo, nil
}

// resolveSections finds the query tenant's streams section and logs sections in an opened object.
// Logs sections are indexed logs-relative across the whole object (both tenants increment the
// counter), matching how the metastore descriptor's SectionIdx is produced and consumed.
func (c *dataObjCache) resolveSections(obj *dataobj.Object) (*openObject, error) {
	oo := &openObject{
		obj:       obj,
		logsSecDO: map[int]*dataobj.Section{},
		logsSec:   map[int]*logs.Section{},
	}

	logsIdx := 0
	for _, sec := range obj.Sections() {
		if sec.Tenant != c.tenant {
			if logs.CheckSection(sec) {
				logsIdx++
			}
			continue
		}
		switch {
		case streams.CheckSection(sec):
			// A data object holds exactly one streams section per tenant (enforced by the builder). A
			// second one would silently drop the streams it holds, so fail the query rather than read an
			// incomplete set of streams.
			if oo.streamsSecDO != nil {
				return nil, fmt.Errorf("multiple streams sections for tenant %q within one data object", c.tenant)
			}
			oo.streamsSecDO = sec
		case logs.CheckSection(sec):
			oo.logsSecDO[logsIdx] = sec
			logsIdx++
		}
	}
	return oo, nil
}

// Close releases the cache. Opened objects and sections hold no resources of their own — reads happen
// through the bucket reader — so this just drops the references.
func (c *dataObjCache) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.byPath)
}

type openObject struct {
	obj *dataobj.Object

	streamsSecDO *dataobj.Section
	logsSecDO    map[int]*dataobj.Section

	// mu guards the lazily-opened sections below. resolveSections fills the descriptor maps above once
	// during single-threaded resolution; the sections here are opened on first use, and one object feeds
	// several concurrent logs-section reads.
	mu         sync.Mutex
	streamsSec *streams.Section
	logsSec    map[int]*logs.Section
}

// streamLabels returns the labels of the requested stream IDs, decoding only the streams the query needs.
//
// filtered reports whether the read was pruned by shard bucket. planSectionRead relies on it: without
// pruning it keeps the fingerprint recheck and treats a listed-but-absent stream as corruption, not as
// out-of-shard.
func (o *openObject) streamLabels(ctx context.Context, want map[streamID]struct{}, query readQuery) (out map[streamID]labels.Labels, filtered bool, err error) {
	out = make(map[streamID]labels.Labels, len(want))
	if o.streamsSecDO == nil || len(want) == 0 {
		return out, false, nil
	}

	sec, err := o.streamsSection(ctx)
	if err != nil {
		return nil, false, err
	}

	wantIDs := streamIDsToInt64(slices.Collect(maps.Keys(want)))
	filtered, err = o.readStreamLabels(ctx, sec, wantIDs, query.shardBucket, out)
	if err != nil {
		return nil, false, err
	}
	return out, filtered, nil
}

// sectionHasColumn reports whether the streams section carries a column of the given type. It reads only
// the already-loaded section metadata, not object storage.
func sectionHasColumn(sec *streams.Section, t streams.ColumnType) bool {
	for _, c := range sec.Columns() {
		if c.Type == t {
			return true
		}
	}
	return false
}

// readStreamLabels decodes the labels of the given stream IDs into out, pushing the IDs down as a
// stream-ID predicate so only those streams are read.
//
// When bucketRange is non-nil and the section carries the __shard_bucket__ column, it also pushes a range
// predicate on that column, dropping out-of-shard streams during the read. When the section is stored
// sorted by shard bucket the predicate prunes whole pages; otherwise it only filters rows after decode.
// filtered reports whether the predicate was pushed; it is false when bucketRange is nil or the object
// predates the column.
func (o *openObject) readStreamLabels(ctx context.Context, sec *streams.Section, ids []int64, bucketRange *shardBucketFilter, out map[streamID]labels.Labels) (filtered bool, err error) {
	if len(ids) == 0 {
		return false, nil
	}

	reader := streams.NewRowReader(sec)
	defer reader.Close()

	if err := reader.MatchStreams(slices.Values(ids)); err != nil {
		return false, err
	}
	if bucketRange != nil && sectionHasColumn(sec, streams.ColumnTypeShardBucket) {
		if err := reader.SetPredicate(streams.ShardBucketRangeRowPredicate{From: bucketRange.from, To: bucketRange.to}); err != nil {
			return false, err
		}
		filtered = true
	}
	if err := reader.Open(ctx); err != nil {
		return false, err
	}

	buf := make([]streams.Stream, 1024)
	for {
		n, err := reader.Read(ctx, buf)
		if err != nil && !errors.Is(err, io.EOF) {
			return false, err
		}
		for i := range buf[:n] {
			out[streamID(buf[i].ID)] = buf[i].Labels
		}
		if n == 0 && errors.Is(err, io.EOF) {
			break
		}
	}
	return filtered, nil
}

// streamsSection opens (and caches) the tenant's streams section, or returns nil if the object has none.
func (o *openObject) streamsSection(ctx context.Context) (*streams.Section, error) {
	o.mu.Lock()
	if o.streamsSec != nil {
		sec := o.streamsSec
		o.mu.Unlock()
		return sec, nil
	}
	secDO := o.streamsSecDO
	o.mu.Unlock()
	if secDO == nil {
		return nil, nil
	}

	// Open outside the lock so it does not block a concurrent logs-section read of the same object.
	sec, err := streams.Open(ctx, secDO)
	if err != nil {
		return nil, err
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.streamsSec != nil {
		return o.streamsSec, nil
	}
	o.streamsSec = sec
	return sec, nil
}

// logsSection opens (and caches) the logs section at the given logs-relative index. It returns nil if
// the object has no such section for the tenant. It is safe for concurrent use.
func (o *openObject) logsSection(ctx context.Context, idx int) (*logs.Section, error) {
	o.mu.Lock()
	if sec, ok := o.logsSec[idx]; ok {
		o.mu.Unlock()
		return sec, nil
	}
	secDO, ok := o.logsSecDO[idx]
	o.mu.Unlock()
	if !ok {
		return nil, nil
	}

	// Open outside the lock so concurrent reads of different sections of the same object don't serialize
	// on section I/O.
	sec, err := logs.Open(ctx, secDO)
	if err != nil {
		return nil, err
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	if existing, ok := o.logsSec[idx]; ok {
		return existing, nil
	}
	o.logsSec[idx] = sec
	return sec, nil
}
