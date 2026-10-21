package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/scalar"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"go.opentelemetry.io/otel"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
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

	// indexPrefetchBytes is how many bytes Sections reads up front from an index object.
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

// ObjectMetastore is a metastore that stores data objects in object storage.
type ObjectMetastore struct {
	bucket      objstore.BucketReader
	parallelism int
	logger      log.Logger
	metrics     *ObjectMetastoreMetrics
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

	// AmbiguousPredicates are predicate names present both as a stream label in
	// the section and in the LogQL query, and are therefore ambiguous. It is the
	// deduped union across the section's streams.
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

// NewSectionDescriptor creates a new section descriptor with the given pointer and labels.
func NewSectionDescriptor(pointer pointers.SectionPointer, ambiguousLabelNames []string) *DataobjSectionDescriptor {
	deduped := dedupeStringSlice(ambiguousLabelNames)
	obj := &DataobjSectionDescriptor{
		SectionKey: SectionKey{
			ObjectPath: pointer.Path,
			SectionIdx: pointer.Section,
		},
		StreamIDs:           []int64{pointer.StreamIDRef},
		RowCount:            int(pointer.LineCount),
		Size:                pointer.UncompressedSize,
		Start:               pointer.StartTs,
		End:                 pointer.EndTs,
		AmbiguousPredicates: deduped,
	}
	return obj
}

// Merge merges the given pointer and labels into an existing section's descriptor.
func (d *DataobjSectionDescriptor) Merge(pointer pointers.SectionPointer, lbls []string) {
	d.StreamIDs = append(d.StreamIDs, pointer.StreamIDRef)
	d.RowCount += int(pointer.LineCount)
	d.Size += pointer.UncompressedSize
	if pointer.StartTs.Before(d.Start) {
		d.Start = pointer.StartTs
	}
	if pointer.EndTs.After(d.End) {
		d.End = pointer.EndTs
	}

	for _, lbl := range lbls {
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

// DataobjSectionDescriptorWithIndex is a section descriptor with the path of the index object
// that listed it.
type DataobjSectionDescriptorWithIndex struct {
	*DataobjSectionDescriptor

	// IndexPath is the path of the index object that listed the section.
	IndexPath string
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

func NewObjectMetastore(b objstore.Bucket, cfg Config, logger log.Logger, metrics *ObjectMetastoreMetrics) *ObjectMetastore {
	if logger == nil {
		logger = log.NewNopLogger()
	}

	if cfg.IndexStoragePrefix != "" {
		b = objstore.NewPrefixedBucket(b, cfg.IndexStoragePrefix)
	}

	store := &ObjectMetastore{
		bucket:      dataobj.NewInstrumentedBucketReader(b),
		parallelism: 64,
		logger:      logger,
		metrics:     metrics,
	}

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
			objects[i], err = m.listObjects(ctx, path, sStart, sEnd)
			// If the metastore object is not found, it means it's outside of any existing window
			// and we can safely ignore it.
			if err != nil && !m.bucket.IsObjNotFoundErr(err) {
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

func (m *ObjectMetastore) listObjects(ctx context.Context, path string, sStart, sEnd *scalar.Timestamp) ([]IndexEntry, error) {
	var buf bytes.Buffer
	objectReader, err := m.bucket.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	defer objectReader.Close()

	n, err := buf.ReadFrom(objectReader)
	if err != nil {
		return nil, fmt.Errorf("reading metastore object: %w", err)
	}
	object, err := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), n)
	if err != nil {
		return nil, fmt.Errorf("getting object from reader: %w", err)
	}
	var entries []IndexEntry

	err = forEachIndexPointer(ctx, object, sStart, sEnd, func(indexPointer indexpointers.IndexPointer) {
		entries = append(entries, IndexEntry{
			Path:  indexPointer.Path,
			Start: indexPointer.StartTs,
			End:   indexPointer.EndTs,
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

// SectionsStream implements [Metastore.SectionsStream].
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

	// batch carries the deduplicated sections of one index object, or the error that ends the
	// resolution. A batch without an error holds at least one section.
	type batch struct {
		sections DataobjSectionDescriptors

		// err is the error that ends the resolution. When err is set, sections is nil.
		err error
	}

	// The resolution runs in the background and never waits for the caller. So its duration excludes
	// the time the caller spends on the batches.
	//
	// The channel holds one batch for each index object, so no send waits for the caller. The batches
	// stay in memory until the caller receives them. The channel closes after the resolution ends.
	readCtx, cancelReads := context.WithCancel(ctx)
	batches := make(chan batch, len(indexes.Indexes))

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

		// fail ends the resolution with err. Call it with mu held.
		fail := func(err error) {
			failed = true

			// err is the only error in the channel. A read that takes mu later finds failed set and
			// sends nothing.
			batches <- batch{err: err}
			cancelReads()
		}

		g.SetLimit(m.parallelism)
		for _, indexEntry := range indexes.Indexes {
			g.Go(func() error {
				sections, readRows, err := m.readIndexSections(readCtx, req, indexEntry.Path)

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

// readIndexSections reads the sections that one index object lists. It returns them with the number
// of rows the index reader resolved, before deduplication.
func (m *ObjectMetastore) readIndexSections(ctx context.Context, req SectionsRequest, indexPath string) ([]DataobjSectionDescriptorWithIndex, int, error) {
	readerResp, err := m.IndexSectionsReader(ctx, IndexSectionsReaderRequest{
		IndexPath:       indexPath,
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
		return nil, 0, fmt.Errorf("collect sections of index object %q: %w", indexPath, err)
	}

	sections := make([]DataobjSectionDescriptorWithIndex, 0, len(sectionsResp.SectionsResponse.Sections))
	for _, section := range sectionsResp.SectionsResponse.Sections {
		sections = append(sections, DataobjSectionDescriptorWithIndex{section, indexPath})
	}

	sp := reader.(statsProvider)
	return sections, int(sp.stats().ReadRows), nil
}

func (m *ObjectMetastore) IndexSectionsReader(ctx context.Context, req IndexSectionsReaderRequest) (IndexSectionsReaderResponse, error) {
	if len(req.SectionsRequest.Matchers) == 0 {
		// At least one stream matcher is required
		return IndexSectionsReaderResponse{}, fmt.Errorf("at least one selector is required")
	}

	idxObj, err := dataobj.FromBucket(ctx, m.bucket, req.IndexPath, req.PrefetchBytes)
	if err != nil {
		return IndexSectionsReaderResponse{}, fmt.Errorf("prepare obj %s: %w", req.IndexPath, err)
	}

	reader := newPostingsIndexSectionsReader(
		m.logger,
		idxObj,
		req.SectionsRequest.Start,
		req.SectionsRequest.End,
		req.SectionsRequest.Matchers,
		req.SectionsRequest.Predicates,
		req.BatchSize,
		req.SectionsRequest.ShardBucketRange,
	)
	return IndexSectionsReaderResponse{Reader: &instrumentedReader{ArrowRecordBatchReader: reader, metrics: m.metrics}}, nil
}

func (m *ObjectMetastore) GetIndexes(ctx context.Context, req GetIndexesRequest) (_ GetIndexesResponse, returnErr error) {
	ctx, span := xcap.StartSpan(ctx, tracer, "metastore.GetIndexes")
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
	numRows := int(rec.NumRows())
	buf := make([]pointers.SectionPointer, numRows)
	num, err := pointers.FromRecordBatch(rec, buf, pointers.PopulateSection)
	if err != nil {
		return fmt.Errorf("convert arrow record to section pointers: %w", err)
	}

	labelsColumn := pointers.InternalLabelsColumn(rec)
	if labelsColumn == nil {
		return fmt.Errorf("internal labels column not found")
	}

	for i := range num {
		ptr := buf[i]
		key := SectionKey{ObjectPath: ptr.Path, SectionIdx: ptr.Section}

		var ambiguousLabels []string
		if !labelsColumn.IsNull(i) {
			ambiguousLabels = strings.Split(labelsColumn.Value(i), ",")
		}

		existing, ok := result[key]
		if !ok {
			result[key] = NewSectionDescriptor(ptr, ambiguousLabels)
			continue
		}
		existing.Merge(ptr, ambiguousLabels)
	}
	return nil
}
