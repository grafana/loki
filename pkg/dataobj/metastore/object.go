package metastore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"maps"
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
	"github.com/grafana/dskit/user"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"go.uber.org/atomic"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/storage/bucket"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
	"github.com/grafana/loki/v3/pkg/xcap"
)

const (
	metastoreWindowSize = 12 * time.Hour
)

type ObjectMetastore struct {
	bucket      objstore.Bucket
	parallelism int
	logger      log.Logger
	metrics     *ObjectMetastoreMetrics
}

type SectionKey struct {
	ObjectPath string
	SectionIdx int64
}

type DataobjSectionDescriptor struct {
	SectionKey

	StreamIDs []int64
	RowCount  int
	Size      int64
	Start     time.Time
	End       time.Time
}

func NewSectionDescriptor(pointer pointers.SectionPointer) *DataobjSectionDescriptor {
	return &DataobjSectionDescriptor{
		SectionKey: SectionKey{
			ObjectPath: pointer.Path,
			SectionIdx: pointer.Section,
		},
		StreamIDs: []int64{pointer.StreamIDRef},
		RowCount:  int(pointer.LineCount),
		Size:      pointer.UncompressedSize,
		Start:     pointer.StartTs,
		End:       pointer.EndTs,
	}
}

func (d *DataobjSectionDescriptor) Merge(pointer pointers.SectionPointer) {
	d.StreamIDs = append(d.StreamIDs, pointer.StreamIDRef)
	d.RowCount += int(pointer.LineCount)
	d.Size += pointer.UncompressedSize
	if pointer.StartTs.Before(d.Start) {
		d.Start = pointer.StartTs
	}
	if pointer.EndTs.After(d.End) {
		d.End = pointer.EndTs
	}
}

// Table of Content files are stored in well-known locations that can be computed from a known time.
func tableOfContentsPath(window time.Time) string {
	return fmt.Sprintf("tocs/%s.toc", strings.ReplaceAll(window.Format(time.RFC3339), ":", "_"))
}

func iterTableOfContentsPaths(start, end time.Time) iter.Seq2[string, multitenancy.TimeRange] {
	minTocWindow := start.Truncate(metastoreWindowSize).UTC()
	maxTocWindow := end.Truncate(metastoreWindowSize).UTC()

	return func(yield func(t string, timeRange multitenancy.TimeRange) bool) {
		for tocWindow := minTocWindow; !tocWindow.After(maxTocWindow); tocWindow = tocWindow.Add(metastoreWindowSize) {
			tocTimeRange := multitenancy.TimeRange{
				MinTime: tocWindow,
				MaxTime: tocWindow.Add(metastoreWindowSize),
			}
			if !yield(tableOfContentsPath(tocWindow), tocTimeRange) {
				return
			}
		}
	}
}

func NewObjectMetastore(b objstore.Bucket, cfg Config, logger log.Logger, metrics *ObjectMetastoreMetrics) *ObjectMetastore {
	if cfg.IndexStoragePrefix != "" {
		b = objstore.NewPrefixedBucket(b, cfg.IndexStoragePrefix)
	}
	b = bucket.NewXCapBucket(b)

	store := &ObjectMetastore{
		bucket:      b,
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

func (m *ObjectMetastore) streams(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]*labels.Labels, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	level.Debug(utillog.WithContext(ctx, m.logger)).Log("msg", "ObjectMetastore.Streams", "tenant", tenantID, "start", start, "end", end, "matchers", matchersToString(matchers))

	// Get all metastore paths for the time range
	var (
		tablePaths []string
	)
	for path := range iterTableOfContentsPaths(start, end) {
		tablePaths = append(tablePaths, path)
	}

	// List objects from all stores concurrently
	paths, err := m.listObjectsFromTables(ctx, tablePaths, start, end)
	if err != nil {
		return nil, err
	}

	// Search the stream sections of the matching objects to find matching streams
	predicate := streamPredicateFromMatchers(start, end, matchers...)
	return m.listStreamsFromObjects(ctx, paths, predicate)
}

func (m *ObjectMetastore) DataObjects(ctx context.Context, start, end time.Time, _ ...*labels.Matcher) ([]string, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	level.Debug(utillog.WithContext(ctx, m.logger)).Log("msg", "ObjectMetastore.DataObjects", "tenant", tenantID, "start", start, "end", end)

	// Get all metastore paths for the time range
	var tablePaths []string
	for path := range iterTableOfContentsPaths(start, end) {
		tablePaths = append(tablePaths, path)
	}

	// List objects from all tables concurrently
	return m.listObjectsFromTables(ctx, tablePaths, start, end)
}

func (m *ObjectMetastore) Labels(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) {
	uniqueLabels := map[string]struct{}{}

	err := m.forEachLabel(ctx, start, end, func(label labels.Label) {
		if _, ok := uniqueLabels[label.Name]; !ok {
			uniqueLabels[label.Name] = struct{}{}
		}
	}, matchers...)

	return slices.Collect(maps.Keys(uniqueLabels)), err
}

func (m *ObjectMetastore) Values(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) {
	values := map[string]struct{}{}

	err := m.forEachLabel(ctx, start, end, func(label labels.Label) {
		if _, ok := values[label.Value]; !ok {
			values[label.Value] = struct{}{}
		}
	}, matchers...)

	return slices.Collect(maps.Keys(values)), err
}

func (m *ObjectMetastore) forEachLabel(ctx context.Context, start, end time.Time, foreach func(labels.Label), matchers ...*labels.Matcher) error {
	streams, err := m.streams(ctx, start, end, matchers...)
	if err != nil {
		return err
	}

	for _, streamLabels := range streams {
		if streamLabels == nil {
			continue
		}

		streamLabels.Range(func(streamLabel labels.Label) {
			foreach(streamLabel)
		})
	}

	return nil
}

func streamPredicateFromMatchers(start, end time.Time, matchers ...*labels.Matcher) streams.RowPredicate {
	if len(matchers) == 0 {
		return nil
	}

	predicates := make([]streams.RowPredicate, 0, len(matchers)+1)
	predicates = append(predicates, streams.TimeRangeRowPredicate{
		StartTime:    start,
		EndTime:      end,
		IncludeStart: true,
		IncludeEnd:   true,
	})
	for _, matcher := range matchers {
		switch matcher.Type {
		case labels.MatchEqual:
			predicates = append(predicates, streams.LabelMatcherRowPredicate{
				Name:  matcher.Name,
				Value: matcher.Value,
			})
		case labels.MatchNotEqual:
			predicates = append(predicates, streams.NotRowPredicate{
				Inner: streams.LabelMatcherRowPredicate{
					Name:  matcher.Name,
					Value: matcher.Value,
				},
			})
		case labels.MatchRegexp:
			predicates = append(predicates, streams.LabelFilterRowPredicate{
				Name: matcher.Name,
				Keep: func(_, value string) bool {
					return matcher.Matches(value)
				},
			})
		case labels.MatchNotRegexp:
			predicates = append(predicates, streams.NotRowPredicate{
				Inner: streams.LabelFilterRowPredicate{
					Name: matcher.Name,
					Keep: func(_, value string) bool {
						return !matcher.Matches(value)
					},
				},
			})
		}
	}

	if len(predicates) == 1 {
		return predicates[0]
	}

	current := predicates[0]

	for _, predicate := range predicates[1:] {
		and := streams.AndRowPredicate{
			Left:  predicate,
			Right: current,
		}
		current = and
	}
	return current
}

// listObjectsFromTables concurrently lists objects from multiple metastore files
func (m *ObjectMetastore) listObjectsFromTables(ctx context.Context, tablePaths []string, start, end time.Time) ([]string, error) {
	objects := make([][]string, len(tablePaths))
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

	return dedupeAndSort(objects), nil
}

func (m *ObjectMetastore) listStreamsFromObjects(ctx context.Context, paths []string, predicate streams.RowPredicate) ([]*labels.Labels, error) {
	mu := sync.Mutex{}
	foundStreams := make(map[uint64][]*labels.Labels, 1024)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(m.parallelism)

	for _, path := range paths {
		g.Go(func() error {
			object, err := dataobj.FromBucket(ctx, m.bucket, path)
			if err != nil {
				return fmt.Errorf("getting object from bucket: %w", err)
			}

			return forEachStream(ctx, object, predicate, func(stream streams.Stream) {
				addLabels(&mu, foundStreams, &stream.Labels)
			})
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	streamsSlice := make([]*labels.Labels, 0, len(foundStreams))
	for _, labels := range foundStreams {
		streamsSlice = append(streamsSlice, labels...)
	}

	return streamsSlice, nil
}

func addLabels(mtx *sync.Mutex, streams map[uint64][]*labels.Labels, newLabels *labels.Labels) {
	mtx.Lock()
	defer mtx.Unlock()

	key := labels.StableHash(*newLabels)
	matches, ok := streams[key]
	if !ok {
		streams[key] = append(streams[key], newLabels)
		return
	}

	for _, lbs := range matches {
		if labels.Equal(*lbs, *newLabels) {
			return
		}
	}
	streams[key] = append(streams[key], newLabels)
}

func (m *ObjectMetastore) listObjects(ctx context.Context, path string, sStart, sEnd *scalar.Timestamp) ([]string, error) {
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
	var objectPaths []string

	err = forEachIndexPointer(ctx, object, sStart, sEnd, func(indexPointer indexpointers.IndexPointer) {
		objectPaths = append(objectPaths, indexPointer.Path)
	})
	if err != nil {
		return nil, err
	}

	return objectPaths, nil
}

func forEachStream(ctx context.Context, object *dataobj.Object, predicate streams.RowPredicate, f func(streams.Stream)) error {
	targetTenant, err := user.ExtractOrgID(ctx)
	if err != nil {
		return fmt.Errorf("extracting org ID: %w", err)
	}
	var reader streams.RowReader
	defer reader.Close()

	buf := make([]streams.Stream, 1024)

	for _, section := range object.Sections().Filter(streams.CheckSection) {
		if section.Tenant != targetTenant {
			continue
		}

		sec, err := streams.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}

		reader.Reset(sec)
		if predicate != nil {
			err := reader.SetPredicate(predicate)
			if err != nil {
				return err
			}
		}
		for {
			num, err := reader.Read(ctx, buf)
			if err != nil && !errors.Is(err, io.EOF) {
				return err
			}
			if num == 0 && errors.Is(err, io.EOF) {
				break
			}
			for _, stream := range buf[:num] {
				f(stream)
			}
		}
	}
	return nil
}

// dedupeAndSort takes a slice of string slices and returns a sorted slice of unique strings
func dedupeAndSort(objects [][]string) []string {
	uniquePaths := make(map[string]struct{})
	for _, batch := range objects {
		for _, path := range batch {
			uniquePaths[path] = struct{}{}
		}
	}

	paths := make([]string, 0, len(uniquePaths))
	for path := range uniquePaths {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func (m *ObjectMetastore) Sections(ctx context.Context, req SectionsRequest) (SectionsResponse, error) {
	ctx, region := xcap.StartRegion(ctx, "ObjectMetastore.Sections")
	defer region.End()

	sectionsTimer := prometheus.NewTimer(m.metrics.resolvedSectionsTotalDuration)

	selector := streamPredicateFromMatchers(req.Start, req.End, req.Matchers...)
	if selector == nil {
		// At least one stream matcher is required, currently.
		return SectionsResponse{}, nil
	}

	indexes, err := m.GetIndexes(ctx, GetIndexesRequest{Start: req.Start, End: req.End})
	if err != nil {
		return SectionsResponse{}, err
	}

	// Return early if no index files found
	if len(indexes.IndexesPaths) == 0 {
		m.metrics.resolvedSectionsTotal.Observe(0)
		level.Debug(utillog.WithContext(ctx, m.logger)).Log("msg", "no sections resolved", "reason", "no index paths")
		return SectionsResponse{}, nil
	}

	var sections []*DataobjSectionDescriptor
	sectionsMu := sync.Mutex{}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(m.parallelism)

	totalSections := atomic.NewUint64(0)
	for _, indexPath := range indexes.IndexesPaths {
		g.Go(func() error {
			readerResp, err := m.IndexSectionsReader(ctx, IndexSectionsReaderRequest{
				IndexPath:       indexPath,
				SectionsRequest: req,
				Region:          region,
			})
			if err != nil {
				return fmt.Errorf("get index sections reader: %w", err)
			}
			reader := readerResp.Reader
			defer reader.Close()
			sectionsResp, err := m.CollectSections(ctx, CollectSectionsRequest{reader})
			if err != nil {
				return fmt.Errorf("collect sections: %w", err)
			}

			// Merge the section descriptors for the object into the global section descriptors in one batch
			sectionsMu.Lock()

			// this is temporary, the stats will be collected differently in a distributed metastore
			statsProvider := reader.(bloomStatsProvider)
			totalSections.Add(statsProvider.totalReadRows())

			sections = append(sections, sectionsResp.SectionsResponse.Sections...)
			sectionsMu.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return SectionsResponse{}, err
	}

	ratio := float64(len(sections)) / float64(totalSections.Load())
	m.metrics.streamFilterSections.Observe(float64(totalSections.Load()))
	m.metrics.resolvedSectionsTotal.Observe(float64(len(sections)))
	m.metrics.resolvedSectionsRatio.Observe(ratio)
	region.Record(xcap.StatMetastoreSectionsResolved.Observe(int64(len(sections))))
	duration := sectionsTimer.ObserveDuration()

	level.Debug(utillog.WithContext(ctx, m.logger)).Log(
		"msg", "resolved sections",
		"duration", duration,
		"tables", len(indexes.TableOfContentsPaths),
		"indexes", len(indexes.IndexesPaths),
		"sections", len(sections),
		"ratio", ratio,
		"matchers", matchersToString(req.Matchers),
		"start", req.Start,
		"end", req.End,
	)

	return SectionsResponse{sections}, nil
}

func (m *ObjectMetastore) IndexSectionsReader(ctx context.Context, req IndexSectionsReaderRequest) (IndexSectionsReaderResponse, error) {
	if len(req.SectionsRequest.Matchers) == 0 {
		// At least one stream matcher is required
		return IndexSectionsReaderResponse{}, fmt.Errorf("at least one selector is required")
	}

	idxObj, err := dataobj.FromBucket(ctx, m.bucket, req.IndexPath)
	if err != nil {
		return IndexSectionsReaderResponse{}, fmt.Errorf("prepare obj %s: %w", req.IndexPath, err)
	}

	sStart := scalar.NewTimestampScalar(arrow.Timestamp(req.SectionsRequest.Start.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(req.SectionsRequest.End.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)

	scanner := newScanPointers(idxObj, sStart, sEnd, req.SectionsRequest.Matchers, req.Region)
	blooms := newApplyBlooms(idxObj, req.SectionsRequest.Predicates, scanner, req.Region)

	return IndexSectionsReaderResponse{Reader: blooms}, nil
}

func (m *ObjectMetastore) GetIndexes(ctx context.Context, req GetIndexesRequest) (GetIndexesResponse, error) {
	ctx, region := xcap.StartRegion(ctx, "ObjectMetastore.GetIndexes")
	defer region.End()

	resp := GetIndexesResponse{}

	// Get all metastore paths for the time range
	for path := range iterTableOfContentsPaths(req.Start, req.End) {
		resp.TableOfContentsPaths = append(resp.TableOfContentsPaths, path)
	}

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
	indexPaths, err := m.listObjectsFromTables(ctx, resp.TableOfContentsPaths, req.Start, req.End)
	if err != nil {
		return resp, err
	}

	resp.IndexesPaths = indexPaths

	m.metrics.indexObjectsTotal.Observe(float64(len(indexPaths)))
	region.Record(xcap.StatMetastoreIndexObjects.Observe(int64(len(indexPaths))))

	return resp, nil
}

func (m *ObjectMetastore) CollectSections(ctx context.Context, req CollectSectionsRequest) (CollectSectionsResponse, error) {
	objectSectionDescriptors := make(map[SectionKey]*DataobjSectionDescriptor)
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

	sections := make([]*DataobjSectionDescriptor, 0, len(objectSectionDescriptors))
	for _, s := range objectSectionDescriptors {
		sections = append(sections, s)
	}

	region := xcap.RegionFromContext(ctx)
	region.Record(xcap.StatMetastoreSectionsResolved.Observe(int64(len(sections))))

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

	for i := range num {
		ptr := buf[i]
		key := SectionKey{ObjectPath: ptr.Path, SectionIdx: ptr.Section}
		existing, ok := result[key]
		if !ok {
			result[key] = NewSectionDescriptor(ptr)
			continue
		}
		existing.Merge(ptr)
	}
	return nil
}
