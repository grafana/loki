package dataobjread

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// DefaultHeadPrefetchBytes is how many bytes an object open reads up front. The encoder packs
// the file metadata and every section's metadata region contiguously at the head, and this
// window is meant to cover them.
const DefaultHeadPrefetchBytes = 256 * 1024

// streamLabelsDecodeBatchSize is how many streams one read of a streams section decodes at a
// time.
const streamLabelsDecodeBatchSize = 1024

// OpenObjects opens each data object once per query and holds its opened sections.
//
// The same object is read first for its stream labels and then for its log rows, so opening it
// once avoids re-reading the object header and section metadata. Lifetime is one query.
type OpenObjects struct {
	bucket        objstore.BucketReader
	tenant        string
	prefetchBytes int64

	// metadataCache holds each object's metadata region. It is shared across queries, and nil
	// when caching is off.
	metadataCache dataobj.MetadataCache

	mu     sync.Mutex
	byPath map[string]*openObject
}

func NewOpenObjects(bucket objstore.BucketReader, tenant string, prefetchBytes int64, metadataCache dataobj.MetadataCache) *OpenObjects {
	return &OpenObjects{
		bucket:        bucket,
		tenant:        tenant,
		prefetchBytes: prefetchBytes,
		metadataCache: metadataCache,
		byPath:        map[string]*openObject{},
	}
}

// get returns the opened object at path, opening it when no caller has yet.
//
// Safe for concurrent use.
func (o *OpenObjects) get(ctx context.Context, path string) (*openObject, error) {
	o.mu.Lock()
	obj, ok := o.byPath[path]
	o.mu.Unlock()
	if ok {
		return obj, nil
	}

	// Open outside the lock so concurrent opens of different objects do not serialize on the
	// object-storage read.
	opened, err := dataobj.FromBucket(ctx, o.bucket, path, o.prefetchBytes, dataobj.WithMetadataCache(o.metadataCache))
	if err != nil {
		return nil, fmt.Errorf("opening data object %q: %w", path, err)
	}
	tenantSections, err := sections.ForTenant(opened.Sections(), o.tenant)
	if err != nil {
		return nil, fmt.Errorf("resolving sections of data object %q: %w", path, err)
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	if existing, ok := o.byPath[path]; ok {
		// Another goroutine opened the same object while this one did. Keep the first, so every
		// caller shares one set of opened sections.
		return existing, nil
	}
	obj = &openObject{path: path, tenant: tenantSections, logsSections: map[int]*logs.Section{}}
	o.byPath[path] = obj
	return obj, nil
}

// release drops the opened objects.
//
// It does not close the registry: a get after release reopens the object.
func (o *OpenObjects) release() {
	o.mu.Lock()
	defer o.mu.Unlock()

	// Objects hold no resources of their own, because reads go through the bucket reader,
	// so we just release the reference.
	clear(o.byPath)
}

// openObject is one opened data object and the sections of it that belong to the query's tenant.
type openObject struct {
	path   string
	tenant sections.TenantSet

	// mu guards the lazily-opened sections below.
	mu             sync.Mutex
	streamsSection *streams.Section
	logsSections   map[int]*logs.Section
}

// streamLabels decodes the labels of the wanted stream IDs, reading only those streams.
//
// The returned label sets may be retained: [streams.NewRowReader] allocates a fresh one per row
// and never hands out a shared buffer.
//
// The read fails when the streams section carries no shard-bucket column, and when the object
// holds no streams section for the tenant at all.
func (o *openObject) streamLabels(ctx context.Context, want []int64, shardBuckets *shardBucketRange) (byID map[int64]labels.Labels, returnErr error) {
	byID = make(map[int64]labels.Labels, len(want))
	if len(want) == 0 {
		return byID, nil
	}
	if o.tenant.Streams == nil {
		// The metastore listed streams of this object, so the object must hold the section they
		// came from.
		return nil, fmt.Errorf("data object %q holds no streams section for the tenant, though %d of its streams were listed", o.path, len(want))
	}

	section, err := o.openStreamsSection(ctx)
	if err != nil {
		return nil, err
	}

	// A section written before the shard-bucket column existed matches no row under the bucket
	// predicate, so reading one would return no stream at all. Failing says so instead of
	// reporting an empty result.
	if !section.HasColumn(streams.ColumnTypeShardBucket) {
		return nil, fmt.Errorf("data object %q has no %s column in its streams section, which this read path requires", o.path, streams.ColumnTypeShardBucket)
	}

	reader := streams.NewRowReader(section)
	defer func() {
		// Report a close failure only when nothing else failed, so the first error wins.
		if closeErr := reader.Close(); returnErr == nil {
			returnErr = closeErr
		}
	}()

	if err := reader.MatchStreams(slices.Values(want)); err != nil {
		return nil, err
	}

	if shardBuckets != nil {
		if err := reader.SetPredicate(streams.ShardBucketRangeRowPredicate{From: uint64(shardBuckets.from), To: uint64(shardBuckets.to)}); err != nil {
			return nil, err
		}
	}

	if err := reader.Open(ctx); err != nil {
		return nil, err
	}

	buf := make([]streams.Stream, streamLabelsDecodeBatchSize)
	for {
		n, err := reader.Read(ctx, buf)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("reading streams of data object %q: %w", o.path, err)
		}
		for i := range buf[:n] {
			byID[buf[i].ID] = buf[i].Labels
		}
		if n == 0 && errors.Is(err, io.EOF) {
			return byID, nil
		}
	}
}

// openStreamsSection opens the tenant's streams section, or returns nil when the object holds
// none for it.
func (o *openObject) openStreamsSection(ctx context.Context) (*streams.Section, error) {
	o.mu.Lock()
	if o.streamsSection != nil {
		section := o.streamsSection
		o.mu.Unlock()
		return section, nil
	}
	descriptor := o.tenant.Streams
	o.mu.Unlock()

	// Open outside the lock so it does not block a concurrent logs-section read of this object.
	section, err := streams.Open(ctx, descriptor)
	if err != nil {
		return nil, fmt.Errorf("opening streams section of data object %q: %w", o.path, err)
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	if o.streamsSection != nil {
		return o.streamsSection, nil
	}
	o.streamsSection = section
	return section, nil
}

// logsSection opens the tenant's logs section at the given logs-relative index. It is safe for
// concurrent use.
//
// An index the object does not hold is an error. The index comes from a metastore descriptor for
// this tenant, so an absent section means the index and the object disagree, and reading nothing
// would under-count the query without saying so.
func (o *openObject) logsSection(ctx context.Context, idx int) (*logs.Section, error) {
	o.mu.Lock()
	if section, ok := o.logsSections[idx]; ok {
		o.mu.Unlock()
		return section, nil
	}
	descriptor, ok := o.tenant.Logs[idx]
	o.mu.Unlock()

	if !ok {
		return nil, fmt.Errorf("data object %q holds no logs section %d for the tenant", o.path, idx)
	}

	// Open outside the lock so concurrent reads of different sections of this object do not
	// serialize on the section read.
	section, err := logs.Open(ctx, descriptor)
	if err != nil {
		return nil, fmt.Errorf("opening logs section %d of data object %q: %w", idx, o.path, err)
	}

	o.mu.Lock()
	defer o.mu.Unlock()
	if existing, ok := o.logsSections[idx]; ok {
		return existing, nil
	}
	o.logsSections[idx] = section
	return section, nil
}
