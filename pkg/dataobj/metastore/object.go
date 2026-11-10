package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"runtime/debug"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/services"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metadatacache"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	serverutil "github.com/grafana/loki/v3/pkg/util/server"
	"github.com/grafana/loki/v3/pkg/xcap"
)

const (
	// MetastoreWindowSize is the duration covered by a single Table of
	// Contents file in the metastore. ToC files are aligned to multiples of
	// this duration. Exported so external packages (e.g. the dataobj
	// compactor) can iterate over the same time windows used internally.
	MetastoreWindowSize = 12 * time.Hour

	// TocPrefix is the prefix under which ToC files are stored in the object
	// storage, laid out as tocs/<window>/<tenant>/toc.toc.
	TocPrefix = "tocs/"

	tocFileName = "toc.toc"

	// indexPrefetchBytes is how many bytes Sections reads up front from an index object. A cache hit
	// replaces this read.
	indexPrefetchBytes = 64 * 1024
)

var (
	tracer = otel.Tracer("pkg/dataobj/metastore")

	// errNoMatchers is the error of a lookup without a stream matcher. The metastore cannot select
	// sections without one, and an empty result would hide the mistake.
	errNoMatchers = errors.New("at least one stream matcher is required")

	// errDifferentSectionStatistics is the error of a lookup that finds one section described by more
	// than one index object, with different statistics.
	errDifferentSectionStatistics = errors.New("the same section was found in multiple index objects with different statistics")
)

// ObjectMetastoreOption customizes an ObjectMetastore.
type ObjectMetastoreOption func(*ObjectMetastore)

// WithIndexMetadataCache serves the metadata region of index objects through cache, or the entire
// object if it is tiny. A nil cache disables it.
func WithIndexMetadataCache(cache dataobj.MetadataCache) ObjectMetastoreOption {
	return func(m *ObjectMetastore) { m.indexMetadataCache = cache }
}

// WithTOCResolverCache makes the ToC resolver of the metastore read through cache when it refreshes a ToC. A nil cache
// disables it.
func WithTOCResolverCache(cache *metadatacache.Cache) ObjectMetastoreOption {
	return func(m *ObjectMetastore) { m.tocCache = cache }
}

// ObjectMetastore is a metastore that stores data objects in object storage.
//
// It is also a service that refreshes the ToCs it keeps in memory. Run it when the ToC resolver is enabled in the
// config. With the resolver disabled, the service does nothing and the metastore reads every ToC from object storage.
type ObjectMetastore struct {
	services.Service

	bucket      objstore.BucketReader
	parallelism int
	logger      log.Logger
	metrics     *ObjectMetastoreMetrics

	// tocResolver serves every ToC the metastore reads. It is never nil.
	tocResolver *TOCResolver

	// tocCache is the shared cache that tocResolver reads through when it refreshes a ToC. It is separate from
	// indexMetadataCache because the resolver rejects a cached ToC that is too old, and indexMetadataCache accepts every
	// cached value.
	tocCache *metadatacache.Cache

	// indexMetadataCache caches the metadata region of index objects. It is nil when no cache is
	// configured.
	//
	// It must never serve a ToC. The writer rewrites a ToC at the same path, so a cached copy would go
	// stale.
	indexMetadataCache dataobj.MetadataCache
}

// SectionKey is a unique identifier for a section of a data object.
type SectionKey struct {
	ObjectPath string
	SectionIdx int64
}

// DataobjSectionDescriptor is a descriptor for single section of a data object, containing some useful information about that section.
type DataobjSectionDescriptor struct {
	SectionKey

	StreamIDs []int64
	RowCount  int
	Size      int64
	Start     time.Time
	End       time.Time

	// AmbiguousPredicates are the names of equality predicates present as a stream label in the
	// section and as a label filter in the LogQL query, and are therefore ambiguous. Other
	// predicate types report no name. It is the deduped union across the section's streams.
	AmbiguousPredicates []string
}

// DataobjSectionDescriptors is a set of section descriptors. A resolution covers every object
// whose sections match, so a set generally holds sections of several objects.
type DataobjSectionDescriptors []*DataobjSectionDescriptor

// ByObject groups the descriptors by the object that holds them.
//
// A stream ID means something only within one object, so a caller that reads streams has to
// work one object at a time. [DataobjSectionDescriptors.StreamIDs] takes one of these groups.
func (d DataobjSectionDescriptors) ByObject() map[string]DataobjSectionDescriptors {
	byObject := make(map[string]DataobjSectionDescriptors)
	for _, descriptor := range d {
		byObject[descriptor.ObjectPath] = append(byObject[descriptor.ObjectPath], descriptor)
	}
	return byObject
}

// StreamIDs returns the stream IDs the descriptors list, deduplicated and in first-seen order.
//
// Call it on one object's descriptors, which [DataobjSectionDescriptors.ByObject] returns. A
// data object's builder assigns its own stream IDs, so the same ID names a different stream in
// another object.
func (d DataobjSectionDescriptors) StreamIDs() []int64 {
	// One object holds far more streams than sections, so size by the stream IDs rather than by
	// the descriptors that carry them.
	var total int
	for _, descriptor := range d {
		total += len(descriptor.StreamIDs)
	}

	var (
		out  = make([]int64, 0, total)
		seen = make(map[int64]struct{})
	)

	for _, descriptor := range d {
		for _, id := range descriptor.StreamIDs {
			if _, duplicate := seen[id]; duplicate {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

// dedupeStringSlice returns s with duplicates removed, order preserved.
func dedupeStringSlice(s []string) []string {
	if len(s) <= 1 {
		return s
	}
	result := make([]string, 0, len(s))
	for _, v := range s {
		if !slices.Contains(result, v) {
			result = append(result, v)
		}
	}
	return result
}

// newSectionDescriptor creates a section descriptor from the given row. It
// removes duplicate ambiguous names.
func newSectionDescriptor(row resolvedRow) *DataobjSectionDescriptor {
	deduped := dedupeStringSlice(row.ambiguousNames)
	obj := &DataobjSectionDescriptor{
		SectionKey: SectionKey{
			ObjectPath: row.objectPath,
			SectionIdx: row.sectionIndex,
		},
		StreamIDs:           []int64{row.streamID},
		RowCount:            int(row.rowCount),
		Size:                row.uncompressedSize,
		Start:               time.Unix(0, row.minTimestamp),
		End:                 time.Unix(0, row.maxTimestamp),
		AmbiguousPredicates: deduped,
	}
	return obj
}

// merge adds row to d. It appends the stream ID, sums the row count and size,
// widens the time range, and adds the ambiguous names that d lacks.
func (d *DataobjSectionDescriptor) merge(row resolvedRow) {
	d.StreamIDs = append(d.StreamIDs, row.streamID)
	d.RowCount += int(row.rowCount)
	d.Size += row.uncompressedSize
	if start := time.Unix(0, row.minTimestamp); start.Before(d.Start) {
		d.Start = start
	}
	if end := time.Unix(0, row.maxTimestamp); end.After(d.End) {
		d.End = end
	}

	for _, lbl := range row.ambiguousNames {
		if !slices.Contains(d.AmbiguousPredicates, lbl) {
			d.AmbiguousPredicates = append(d.AmbiguousPredicates, lbl)
		}
	}
}

// sameStreamAndStatistics reports whether two descriptors of one section have the same streams,
// row count and size.
func (d *DataobjSectionDescriptor) sameStreamAndStatistics(other *DataobjSectionDescriptor) bool {
	// The postings flow reports no row count and no size, so it skips a zero value.
	return slices.Equal(sortedIDs(d.StreamIDs), sortedIDs(other.StreamIDs)) &&
		(d.RowCount == 0 || other.RowCount == 0 || d.RowCount == other.RowCount) &&
		(d.Size == 0 || other.Size == 0 || d.Size == other.Size)
}

// DataobjSectionDescriptorWithIndex is a section descriptor with the logical index that listed
// it.
type DataobjSectionDescriptorWithIndex struct {
	*DataobjSectionDescriptor

	// Index describes the logical index that listed the section, for logs.
	Index string
}

// sortedIDs returns the IDs in ascending order. It returns ids itself when they already are, so
// the caller must not modify the result. It never modifies ids.
func sortedIDs(ids []int64) []int64 {
	if slices.IsSorted(ids) {
		return ids
	}
	return slices.Sorted(slices.Values(ids))
}

// TableOfContentsPath returns the path of the tenant's ToC for window.
// window must be aligned to MetastoreWindowSize.
func TableOfContentsPath(tenant string, window time.Time) string {
	return TableOfContentsWindowPrefix(window) + tenant + "/" + tocFileName
}

// TableOfContentsWindowPrefix returns the folder holding every tenant's ToC for window.
// window must be aligned to MetastoreWindowSize.
func TableOfContentsWindowPrefix(window time.Time) string {
	return TocPrefix + strings.ReplaceAll(window.Format(time.RFC3339), ":", "_") + "/"
}

// ListTableOfContentsTenants returns the tenants that have a ToC for window, sorted.
// An empty window returns no tenants and no error.
func ListTableOfContentsTenants(ctx context.Context, bucket objstore.BucketReader, window time.Time) ([]string, error) {
	prefix := TableOfContentsWindowPrefix(window)

	var tenants []string
	err := bucket.Iter(ctx, prefix, func(name string) error {
		// Each tenant's ToC sits in its own folder; skip anything else.
		tenant, ok := strings.CutSuffix(strings.TrimPrefix(name, prefix), "/")
		if ok && tenant != "" && !strings.Contains(tenant, "/") {
			tenants = append(tenants, tenant)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("listing ToCs under %s: %w", prefix, err)
	}
	slices.Sort(tenants)
	return tenants, nil
}

// IterTableOfContentsPaths returns the paths of the tenant's ToCs for every
// window that overlaps [start, end]. start and end may be unaligned; the
// iterator truncates them to MetastoreWindowSize boundaries.
func IterTableOfContentsPaths(tenant string, start, end time.Time) iter.Seq[string] {
	minTocWindow := start.Truncate(MetastoreWindowSize).UTC()
	maxTocWindow := end.Truncate(MetastoreWindowSize).UTC()

	return func(yield func(string) bool) {
		for tocWindow := minTocWindow; !tocWindow.After(maxTocWindow); tocWindow = tocWindow.Add(MetastoreWindowSize) {
			if !yield(TableOfContentsPath(tenant, tocWindow)) {
				return
			}
		}
	}
}

// NewObjectMetastore returns a metastore that reads tables of contents and index objects under [IndexStoragePrefix] in
// b. It registers its metrics, and those of its ToC resolver, on reg. reg may be nil, and a registerer accepts one
// metastore.
func NewObjectMetastore(b objstore.Bucket, cfg Config, logger log.Logger, reg prometheus.Registerer, opts ...ObjectMetastoreOption) *ObjectMetastore {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	store := &ObjectMetastore{
		bucket:      dataobj.NewInstrumentedBucketReader(NewIndexBucket(b)),
		parallelism: 64,
		logger:      logger,
		metrics:     NewObjectMetastoreMetrics(reg),
	}
	for _, opt := range opts {
		opt(store)
	}

	// The resolver reads the bucket of the metastore. Its cache keys carry the same index storage prefix.
	store.tocResolver = NewTOCResolver(store.bucket, cfg.TOCResolver, store.tocCache, IndexStoragePrefix+"/", logger, reg)
	store.Service = store.tocResolver.Service

	return store
}

func matchersToString(matchers []*labels.Matcher) string {
	var s strings.Builder
	s.WriteString("{")
	for i, m := range matchers {
		if i > 0 {
			s.WriteString(",")
		}
		s.WriteString(m.String())
	}
	s.WriteString("}")
	return s.String()
}

func (m *ObjectMetastore) DataObjects(ctx context.Context, start, end time.Time, _ ...*labels.Matcher) ([]string, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	level.Debug(utillog.WithContext(ctx, m.logger)).Log("msg", "ObjectMetastore.DataObjects", "tenant", tenantID, "start", start, "end", end)

	// Get all metastore paths for the time range
	var tablePaths []string
	for path := range IterTableOfContentsPaths(tenantID, start, end) {
		tablePaths = append(tablePaths, path)
	}

	// List objects from all tables concurrently
	entries, err := m.listObjectsFromTables(ctx, tablePaths, start, end)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		paths = append(paths, entry.Path)
	}
	return paths, nil
}

// listObjectsFromTables concurrently lists objects from multiple metastore files
func (m *ObjectMetastore) listObjectsFromTables(ctx context.Context, tablePaths []string, start, end time.Time) ([]IndexEntry, error) {
	objects := make([][]IndexEntry, len(tablePaths))
	g, ctx := errgroup.WithContext(ctx)

	sStart := scalar.NewTimestampScalar(arrow.Timestamp(start.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(end.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)

	for i, path := range tablePaths {
		g.Go(func() error {
			var err error
			// A missing ToC means the window holds no data, so listObjects returns no entries and no error.
			objects[i], err = m.listObjects(ctx, path, sStart, sEnd)
			if err != nil {
				return fmt.Errorf("listing objects from metastore %s: %w", path, err)
			}
			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	return dedupeAndSortEntries(objects), nil
}

// listObjects returns the entries of the ToC at path that overlap [sStart, sEnd] and belong to the tenant in ctx. A
// missing ToC yields no entries.
func (m *ObjectMetastore) listObjects(ctx context.Context, path string, sStart, sEnd *scalar.Timestamp) ([]IndexEntry, error) {
	data, err := m.tocResolver.ReadTOC(ctx, path)
	if err != nil {
		return nil, err
	}

	// The resolver returns no bytes for a ToC that does not exist, which means the window holds no data.
	if len(data) == 0 {
		return nil, nil
	}

	object, err := dataobj.FromReaderAt(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("getting object from reader: %w", err)
	}
	var entries []IndexEntry

	err = forEachIndexPointer(ctx, object, sStart, sEnd, func(indexPointer indexpointers.IndexPointer) {
		entries = append(entries, IndexEntry{
			Path:                indexPointer.Path,
			Start:               indexPointer.StartTs,
			End:                 indexPointer.EndTs,
			ShardBuckets:        indexPointer.ShardBuckets,
			TableOfContentsPath: path,
			RunID:               indexPointer.RunID,
			PostingKeys:         indexPointer.PostingKeys,
		})
	})
	if err != nil {
		return nil, err
	}

	return entries, nil
}

// dedupeAndSortEntries takes a slice of IndexEntry slices and returns a sorted slice of unique entries (by path).
func dedupeAndSortEntries(batches [][]IndexEntry) []IndexEntry {
	uniqueEntries := make(map[string]IndexEntry)
	for _, batch := range batches {
		for _, entry := range batch {
			// This should not happen, but just in case, adjust the index time range to cover the union of the time ranges.
			// E.g. if first entry covers [T0, T10] and second [T5, T15], the final index time range should be [T0, T15].
			if existing, ok := uniqueEntries[entry.Path]; ok {
				if entry.Start.Before(existing.Start) {
					existing.Start = entry.Start
				}
				if entry.End.After(existing.End) {
					existing.End = entry.End
				}
				uniqueEntries[entry.Path] = existing
				continue
			}

			uniqueEntries[entry.Path] = entry
		}
	}

	entries := make([]IndexEntry, 0, len(uniqueEntries))
	for _, entry := range uniqueEntries {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Path < entries[j].Path
	})
	return entries
}

// Sections collects every batch of SectionsStream. It returns no section when the sequence fails.
func (m *ObjectMetastore) Sections(ctx context.Context, req SectionsRequest) (SectionsResponse, error) {
	var sections DataobjSectionDescriptors
	for batch, err := range m.SectionsStream(ctx, req) {
		if err != nil {
			return SectionsResponse{}, err
		}
		sections = append(sections, batch...)
	}
	return SectionsResponse{Sections: sections}, nil
}

func (m *ObjectMetastore) SectionsStream(ctx context.Context, req SectionsRequest) iter.Seq2[DataobjSectionDescriptors, error] {
	return func(yield func(DataobjSectionDescriptors, error) bool) {
		ctx, span := xcap.StartSpan(ctx, tracer, RegionSections)
		defer span.End()

		m.streamSections(ctx, span, req, yield)
	}
}

// streamSections runs one resolution and yields its batches from the calling goroutine. It returns
// after every read ends.
func (m *ObjectMetastore) streamSections(ctx context.Context, span *xcap.Span, req SectionsRequest, yield func(DataobjSectionDescriptors, error) bool) {
	start := time.Now()

	if len(req.Matchers) == 0 {
		yield(nil, errNoMatchers)
		return
	}

	indexes, err := m.GetIndexes(ctx, GetIndexesRequest{Start: req.Start, End: req.End})
	if err != nil {
		yield(nil, err)
		return
	}

	// Return early if no index files found
	if len(indexes.Indexes) == 0 {
		m.metrics.resolvedSectionsTotal.Observe(0)
		level.Debug(utillog.WithContext(ctx, m.logger)).Log("msg", "no sections resolved", "reason", "no index paths")
		span.Record(StatMetastoreSectionsDuration.Observe(int64(time.Since(start))))
		return
	}

	// Read each logical index with one reader, and only its index objects that can hold a posting
	// that the request reads.
	var (
		toRead  []logicalIndex
		skipped int
	)
	for _, index := range logicalIndexes(indexes.Indexes) {
		kept, ok := index.withPostingsFor(req.Matchers, req.Predicates)
		skipped += len(index.entries) - len(kept.entries)
		if ok {
			toRead = append(toRead, kept)
		}
	}
	m.metrics.skippedIndexObjects.Observe(float64(skipped))

	if len(toRead) == 0 {
		m.metrics.resolvedSectionsTotal.Observe(0)
		level.Debug(utillog.WithContext(ctx, m.logger)).Log("msg", "no sections resolved", "reason", "no index object holds postings of the matchers")
		span.Record(StatMetastoreSectionsDuration.Observe(int64(time.Since(start))))
		return
	}

	// batch carries the deduplicated sections of one logical index, or the error that ends the
	// resolution. A batch without an error holds at least one section.
	type batch struct {
		sections DataobjSectionDescriptors

		// err is the error that ends the resolution. When err is set, sections is nil.
		err error
	}

	// The resolution runs in the background and never waits for the caller. So its duration excludes
	// the time the caller spends on the batches.
	//
	// The channel holds one batch for each logical index, so no send waits for the caller. The
	// batches stay in memory until the caller receives them. The channel closes after the resolution
	// ends.
	readCtx, cancelReads := context.WithCancel(ctx)
	batches := make(chan batch, len(toRead))

	go func() {
		defer close(batches)

		var (
			g errgroup.Group

			// mu guards the variables below.
			mu sync.Mutex

			// deduplicator keeps the first descriptor of each section until the resolution ends. It
			// compares each later copy with that descriptor, which the caller can already hold.
			deduplicator = newSectionsDeduplicator(m.logger, m.metrics)

			failed         bool
			totalSections  int
			uniqueSections int
		)

		// fail ends the resolution with err. It does nothing when an earlier read already failed. Call
		// it with mu held.
		fail := func(err error) {
			if failed {
				return
			}
			failed = true

			// The check above makes err the only error in the channel.
			batches <- batch{err: err}
			cancelReads()
		}

		g.SetLimit(m.parallelism)
		for _, index := range toRead {
			g.Go(func() error {
				// The caller cannot recover a panic on this goroutine, and the panic would end the
				// process. End the resolution with an error instead. The deferred Unlock below runs
				// first, so mu is free here.
				defer func() {
					if panicked := recover(); panicked != nil {
						serverutil.RecordPanic()
						level.Error(utillog.WithContext(ctx, m.logger)).Log(
							"msg", "panic reading the sections of an index object",
							"index", index.String(),
							"panic", panicked,
							"stack", string(debug.Stack()),
						)

						mu.Lock()
						defer mu.Unlock()
						fail(fmt.Errorf("reading the sections of %s: %v", index, panicked))
					}
				}()

				sections, readRows, err := m.readIndexSections(readCtx, req, index)

				mu.Lock()
				defer mu.Unlock()

				// An earlier read failed. The deduplicator must not run again after an error, and the
				// caller drops the batches that follow.
				if failed {
					return nil
				}

				if err != nil {
					fail(err)
					return nil
				}

				indexUniqueSections, err := deduplicator.deduplicate(ctx, sections)
				if err != nil {
					fail(err)
					return nil
				}

				totalSections += readRows
				if len(indexUniqueSections) == 0 {
					return nil
				}

				if uniqueSections == 0 {
					firstIndex := time.Since(start)
					m.metrics.resolvedSectionsFirstIndexDuration.Observe(firstIndex.Seconds())
					span.Record(StatMetastoreSectionsFirstIndexDuration.Observe(int64(firstIndex)))
				}
				uniqueSections += len(indexUniqueSections)

				batches <- batch{sections: indexUniqueSections}
				return nil
			})
		}
		_ = g.Wait()

		// A failed or canceled resolution skips the totals below. A read can succeed after the cancel,
		// so check the context as well as the failed flag.
		if failed || readCtx.Err() != nil {
			return
		}

		m.metrics.streamFilterSections.Observe(float64(totalSections))
		m.metrics.resolvedSectionsTotal.Observe(float64(uniqueSections))

		// The stream filters can resolve no section, and then the ratio is undefined. Observing 0/0
		// would turn the histogram sum into NaN. Observing 0 would claim that deduplication dropped
		// every section.
		var ratio float64
		if totalSections > 0 {
			ratio = float64(uniqueSections) / float64(totalSections)
			m.metrics.resolvedSectionsRatio.Observe(ratio)
		}
		duration := time.Since(start)
		m.metrics.resolvedSectionsTotalDuration.Observe(duration.Seconds())
		span.Record(StatMetastoreSectionsDuration.Observe(int64(duration)))

		level.Debug(utillog.WithContext(ctx, m.logger)).Log(
			"msg", "resolved sections",
			"duration", duration,
			"tables", len(indexes.TableOfContentsPaths),
			"indexes", len(indexes.Indexes),
			"skipped_indexes", skipped,
			"sections", uniqueSections,
			"ratio", ratio,
			"matchers", matchersToString(req.Matchers),
			"start", req.Start,
			"end", req.End,
		)
	}()

	// On return, cancel the reads and drop the batches left in the channel. The channel closes only
	// after every read ends, so this loop also waits for the reads. It also makes every span.Record
	// of the resolution happen before span.End.
	defer func() {
		cancelReads()
		for range batches {
		}
	}()

	for b := range batches {
		if b.err != nil {
			yield(nil, b.err)
			return
		}
		if !yield(b.sections, nil) {
			return
		}
	}
}

// readIndexSections reads the sections that one logical index lists. It returns them with the
// number of rows the index reader resolved, before deduplication.
func (m *ObjectMetastore) readIndexSections(ctx context.Context, req SectionsRequest, index logicalIndex) ([]DataobjSectionDescriptorWithIndex, int, error) {
	readerResp, err := m.IndexSectionsReader(ctx, IndexSectionsReaderRequest{
		Indexes:         index.entries,
		SectionsRequest: req,
		PrefetchBytes:   indexPrefetchBytes,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("get index sections reader: %w", err)
	}
	reader := readerResp.Reader
	defer reader.Close()

	sectionsResp, err := m.CollectSections(ctx, CollectSectionsRequest{reader})
	if err != nil {
		return nil, 0, fmt.Errorf("collect sections of %s: %w", index, err)
	}

	name := index.String()
	sections := make([]DataobjSectionDescriptorWithIndex, 0, len(sectionsResp.SectionsResponse.Sections))
	for _, section := range sectionsResp.SectionsResponse.Sections {
		sections = append(sections, DataobjSectionDescriptorWithIndex{section, name})
	}

	sp := reader.(statsProvider)
	return sections, int(sp.stats().ReadRows), nil
}

func (m *ObjectMetastore) IndexSectionsReader(ctx context.Context, req IndexSectionsReaderRequest) (IndexSectionsReaderResponse, error) {
	if len(req.SectionsRequest.Matchers) == 0 {
		// At least one stream matcher is required
		return IndexSectionsReaderResponse{}, fmt.Errorf("at least one selector is required")
	}

	if len(req.Indexes) == 0 {
		return IndexSectionsReaderResponse{}, fmt.Errorf("at least one index object is required")
	}

	objects, err := m.openIndexObjects(ctx, req.Indexes, req.PrefetchBytes)
	if err != nil {
		return IndexSectionsReaderResponse{}, err
	}

	reader := newPostingsIndexSectionsReader(
		m.logger,
		objects,
		req.SectionsRequest.Start,
		req.SectionsRequest.End,
		req.SectionsRequest.Matchers,
		req.SectionsRequest.Predicates,
		req.BatchSize,
		req.SectionsRequest.StreamHashRange,
	)
	return IndexSectionsReaderResponse{Reader: &instrumentedReader{ArrowRecordBatchReader: reader, metrics: m.metrics}}, nil
}

// openIndexObjects opens the index objects of entries, in parallel if there is
// more than one.
//
// A panic of a parallel open happens again on the calling goroutine, after
// every open ends. So the caller recovers it as it recovers a panic of its own
// reads. One index object opens on the calling goroutine, so its panic keeps
// the stack of the read.
func (m *ObjectMetastore) openIndexObjects(ctx context.Context, entries []IndexEntry, prefetchBytes int64) ([]indexObject, error) {
	open := func(ctx context.Context, entry IndexEntry) (indexObject, error) {
		obj, err := dataobj.FromBucket(ctx, m.bucket, entry.Path, prefetchBytes, dataobj.WithMetadataCache(m.indexMetadataCache))
		if err != nil {
			return indexObject{}, fmt.Errorf("prepare obj %s: %w", entry.Path, err)
		}
		return indexObject{obj: obj, shardBuckets: entry.ShardBuckets}, nil
	}
	if len(entries) == 1 {
		object, err := open(ctx, entries[0])
		if err != nil {
			return nil, err
		}
		return []indexObject{object}, nil
	}

	var (
		objects = make([]indexObject, len(entries))

		mu       sync.Mutex
		panicked any
	)
	g, groupCtx := errgroup.WithContext(ctx)
	for i, entry := range entries {
		g.Go(func() error {
			defer func() {
				if p := recover(); p != nil {
					mu.Lock()
					defer mu.Unlock()
					if panicked == nil {
						panicked = p
					}
				}
			}()

			object, err := open(groupCtx, entry)
			if err != nil {
				return err
			}
			objects[i] = object
			return nil
		})
	}
	err := g.Wait()
	if panicked != nil {
		panic(panicked)
	}
	return objects, err
}

func (m *ObjectMetastore) GetIndexes(ctx context.Context, req GetIndexesRequest) (_ GetIndexesResponse, returnErr error) {
	ctx, span := xcap.StartSpan(ctx, tracer, RegionGetIndexes)
	defer span.End()
	start := time.Now()
	defer func() {
		m.metrics.getIndexesTotalDuration.WithLabelValues(getIndexesResult(returnErr)).Observe(time.Since(start).Seconds())
	}()

	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return GetIndexesResponse{}, err
	}

	resp := GetIndexesResponse{}

	// Get all metastore paths for the time range
	for path := range IterTableOfContentsPaths(tenantID, req.Start, req.End) {
		resp.TableOfContentsPaths = append(resp.TableOfContentsPaths, path)
	}
	span.Record(StatMetastoreTocTables.Observe(int64(len(resp.TableOfContentsPaths))))

	// Return early if no toc files are found
	if len(resp.TableOfContentsPaths) == 0 {
		m.metrics.indexObjectsTotal.Observe(0)
		m.metrics.resolvedSectionsTotal.Observe(0)
		level.Warn(utillog.WithContext(ctx, m.logger)).Log(
			"msg", "no sections resolved",
			"reason", "no toc paths",
			"start", req.Start,
			"end", req.End,
		)
		return GetIndexesResponse{}, nil
	}

	// List index objects from all tables concurrently
	indexEntries, err := m.listObjectsFromTables(ctx, resp.TableOfContentsPaths, req.Start, req.End)
	if err != nil {
		return resp, err
	}

	resp.Indexes = indexEntries

	m.metrics.indexObjectsTotal.Observe(float64(len(indexEntries)))
	span.Record(StatMetastoreIndexObjects.Observe(int64(len(indexEntries))))

	return resp, nil
}

// CollectSections reads every batch from req.Reader and merges the rows into
// one descriptor per section. It returns an error when a non-empty batch is not
// in postingsResultSchema.
func (m *ObjectMetastore) CollectSections(ctx context.Context, req CollectSectionsRequest) (CollectSectionsResponse, error) {
	objectSectionDescriptors := make(map[SectionKey]*DataobjSectionDescriptor)

	if err := req.Reader.Open(ctx); err != nil {
		return CollectSectionsResponse{}, fmt.Errorf("opening reader: %w", err)
	}

	for {
		rec, err := req.Reader.Read(ctx)
		if err != nil && !errors.Is(err, io.EOF) {
			level.Warn(m.logger).Log(
				"msg", "error during execution",
				"err", err,
			)
			return CollectSectionsResponse{}, err
		}

		if rec != nil && rec.NumRows() > 0 {
			if err := addSectionDescriptors(rec, objectSectionDescriptors); err != nil {
				return CollectSectionsResponse{}, err
			}
		}

		if errors.Is(err, io.EOF) {
			break
		}
	}

	m.metrics.resolvedSectionsPerObject.Observe(float64(len(objectSectionDescriptors)))

	sections := make([]*DataobjSectionDescriptor, 0, len(objectSectionDescriptors))
	for _, s := range objectSectionDescriptors {
		sections = append(sections, s)
	}

	return CollectSectionsResponse{
		SectionsResponse: SectionsResponse{
			Sections: sections,
		},
	}, nil
}

func addSectionDescriptors(rec arrow.RecordBatch, result map[SectionKey]*DataobjSectionDescriptor) error {
	rows, err := resolvedRowsFromRecordBatch(rec)
	if err != nil {
		return fmt.Errorf("decode resolved rows: %w", err)
	}

	for _, row := range rows {
		key := SectionKey{ObjectPath: row.objectPath, SectionIdx: row.sectionIndex}
		existing, ok := result[key]
		if !ok {
			result[key] = newSectionDescriptor(row)
			continue
		}
		existing.merge(row)
	}
	return nil
}

// IndexStoragePrefix is the object storage prefix of the index objects and the
// tables of contents.
const IndexStoragePrefix = "index/v0"

// NewIndexBucket returns b scoped to [IndexStoragePrefix].
//
// Use it to write or read index objects and tables of contents without a
// metastore. [NewObjectMetastore] adds the prefix itself, so pass it the
// unwrapped bucket.
func NewIndexBucket(b objstore.Bucket) objstore.Bucket {
	return objstore.NewPrefixedBucket(b, IndexStoragePrefix)
}
