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
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
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
	metrics     *objectMetastoreMetrics
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

func NewObjectMetastore(bucket objstore.Bucket, logger log.Logger, reg prometheus.Registerer) *ObjectMetastore {
	store := &ObjectMetastore{
		bucket:      bucket,
		parallelism: 64,
		logger:      logger,
		metrics:     newObjectMetastoreMetrics(),
	}
	if reg != nil {
		store.metrics.register(reg)
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

func (m *ObjectMetastore) Sections(ctx context.Context, start, end time.Time, matchers []*labels.Matcher, predicates []*labels.Matcher) ([]*DataobjSectionDescriptor, error) {
	ctx, region := xcap.StartRegion(ctx, "ObjectMetastore.Sections")
	defer region.End()

	sectionsTimer := prometheus.NewTimer(m.metrics.resolvedSectionsTotalDuration)

	// Get all metastore paths for the time range
	var tablePaths []string
	for path := range iterTableOfContentsPaths(start, end) {
		tablePaths = append(tablePaths, path)
	}

	// Return early if no toc files are found
	if len(tablePaths) == 0 {
		m.metrics.indexObjectsTotal.Observe(0)
		m.metrics.resolvedSectionsTotal.Observe(0)
		level.Debug(utillog.WithContext(ctx, m.logger)).Log("msg", "no sections resolved", "reason", "no toc paths")
		return nil, nil
	}

	// List index objects from all tables concurrently
	indexPaths, err := m.listObjectsFromTables(ctx, tablePaths, start, end)
	if err != nil {
		return nil, err
	}

	m.metrics.indexObjectsTotal.Observe(float64(len(indexPaths)))
	region.Record(xcap.StatMetastoreIndexObjects.Observe(int64(len(indexPaths))))

	// Return early if no index files are found
	if len(indexPaths) == 0 {
		m.metrics.resolvedSectionsTotal.Observe(0)
		level.Debug(utillog.WithContext(ctx, m.logger)).Log("msg", "no sections resolved", "reason", "no index paths")
		return nil, nil
	}

	// init index files
	indexObjects := make([]*dataobj.Object, len(indexPaths))
	g, initCtx := errgroup.WithContext(ctx)
	g.SetLimit(m.parallelism)
	for idx, indexPath := range indexPaths {
		g.Go(func() error {
			indexObjects[idx], err = dataobj.FromBucket(initCtx, m.bucket, indexPath)
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}

	// Search the stream sections of the matching objects to find matching streams
	streamSectionPointers, err := m.getSectionsForStreams(ctx, indexObjects, start, end, matchers)
	if err != nil {
		return nil, err
	}
	initialSectionPointersCount := len(streamSectionPointers)

	if len(predicates) > 0 {
		// Search the section AMQs to estimate sections that might match the predicates
		// AMQs may return false positives so this is an over-estimate.
		//
		// Only match one predicate at a time in order to obtain the intersection of all estimates, rather than the union.
		for _, predicate := range predicates {
			if predicate.Type != labels.MatchEqual {
				continue
			}
			matchedSections, err := m.estimateSectionsForMatcher(ctx, indexObjects, predicate)
			if err != nil {
				return nil, err
			}

			streamSectionPointers = intersectSections(streamSectionPointers, matchedSections)
			if len(streamSectionPointers) == 0 {
				level.Debug(utillog.WithContext(ctx, m.logger)).Log("msg", "no sections resolved", "reason", "no matching predicates")
				// Short circuit here if no sections match the predicates
				return streamSectionPointers, nil
			}
		}
	}

	duration := sectionsTimer.ObserveDuration()
	m.metrics.resolvedSectionsTotal.Observe(float64(len(streamSectionPointers)))
	m.metrics.resolvedSectionsRatio.Observe(float64(len(streamSectionPointers)) / float64(initialSectionPointersCount))
	region.Record(xcap.StatMetastoreResolvedSections.Observe(int64(len(streamSectionPointers))))

	level.Debug(utillog.WithContext(ctx, m.logger)).Log(
		"msg", "resolved sections",
		"duration", duration,
		"tables", len(tablePaths),
		"indexes", len(indexPaths),
		"sections", len(streamSectionPointers),
		"ratio", float64(len(streamSectionPointers))/float64(initialSectionPointersCount),
		"matchers", matchersToString(matchers),
		"start", start,
		"end", end,
	)

	return streamSectionPointers, nil
}

func intersectSections(sectionPointers []*DataobjSectionDescriptor, sectionMembershipEstimates []SectionKey) []*DataobjSectionDescriptor {
	existence := make(map[SectionKey]struct{}, len(sectionMembershipEstimates))
	for _, section := range sectionMembershipEstimates {
		existence[section] = struct{}{}
	}

	nextEmptyIdx := 0
	key := SectionKey{}
	for _, section := range sectionPointers {
		key.ObjectPath = section.ObjectPath
		key.SectionIdx = section.SectionIdx
		if _, ok := existence[key]; ok {
			sectionPointers[nextEmptyIdx] = section
			nextEmptyIdx++
		}
	}
	return sectionPointers[:nextEmptyIdx]
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

func (m *ObjectMetastore) listStreamIDsFromLogObjects(ctx context.Context, objectPaths []string, start, end time.Time, matchers []*labels.Matcher) ([][]int64, []int, error) {
	streamIDs := make([][]int64, len(objectPaths))
	sections := make([]int, len(objectPaths))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(m.parallelism)

	for idx, objectPath := range objectPaths {
		g.Go(func() error {
			object, err := dataobj.FromBucket(ctx, m.bucket, objectPath)
			if err != nil {
				return fmt.Errorf("getting object from bucket: %w", err)
			}

			sections[idx] = object.Sections().Count(logs.CheckSection)
			streamIDs[idx] = make([]int64, 0, 8)

			return forEachStreamID(ctx, object, start, end, matchers, func(streamID int64) {
				streamIDs[idx] = append(streamIDs[idx], streamID)
			})
		})
	}

	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	return streamIDs, sections, nil
}

// getSectionsForStreams reads the section data from matching streams and aggregates them into section descriptors.
// This is an exact lookup and includes metadata from the streams in each section: the stream IDs, the min-max timestamps, the number of bytes & number of lines.
func (m *ObjectMetastore) getSectionsForStreams(ctx context.Context, indexObjects []*dataobj.Object, start, end time.Time, matchers []*labels.Matcher) ([]*DataobjSectionDescriptor, error) {
	if len(matchers) == 0 {
		// At least one stream matcher is required, currently.
		return nil, nil
	}

	sStart := scalar.NewTimestampScalar(arrow.Timestamp(start.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)
	sEnd := scalar.NewTimestampScalar(arrow.Timestamp(end.UnixNano()), arrow.FixedWidthTypes.Timestamp_ns)

	timer := prometheus.NewTimer(m.metrics.streamFilterTotalDuration)
	defer timer.ObserveDuration()

	var sectionDescriptors []*DataobjSectionDescriptor

	sectionDescriptorsMutex := sync.Mutex{}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(m.parallelism)

	for _, indexObject := range indexObjects {
		g.Go(func() error {
			var key SectionKey
			var matchingStreamIDs []int64

			streamReadTimer := prometheus.NewTimer(m.metrics.streamFilterStreamsReadDuration)
			err := forEachStreamID(ctx, indexObject, start, end, matchers, func(streamID int64) {
				matchingStreamIDs = append(matchingStreamIDs, streamID)
			})
			if err != nil {
				return fmt.Errorf("reading streams from index: %w", err)
			}
			streamReadTimer.ObserveDuration()

			if len(matchingStreamIDs) == 0 {
				// No streams match, so skip reading the section pointers or we'll match all of them.
				return nil
			}

			objectSectionDescriptors := make(map[SectionKey]*DataobjSectionDescriptor)
			sectionPointerReadTimer := prometheus.NewTimer(m.metrics.streamFilterPointersReadDuration)

			err = forEachStreamSectionPointer(ctx, indexObject, sStart, sEnd, matchingStreamIDs, func(pointer pointers.SectionPointer) {
				key.ObjectPath = pointer.Path
				key.SectionIdx = pointer.Section

				sectionDescriptor, ok := objectSectionDescriptors[key]
				if !ok {
					objectSectionDescriptors[key] = NewSectionDescriptor(pointer)
					return
				}
				sectionDescriptor.Merge(pointer)
			})

			if err != nil {
				return fmt.Errorf("reading section pointers from index: %w", err)
			}
			sectionPointerReadTimer.ObserveDuration()

			// Merge the section descriptors for the object into the global section descriptors in one batch
			sectionDescriptorsMutex.Lock()
			for _, sectionDescriptor := range objectSectionDescriptors {
				sectionDescriptors = append(sectionDescriptors, sectionDescriptor)
			}
			sectionDescriptorsMutex.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	m.metrics.streamFilterSections.Observe(float64(len(sectionDescriptors)))
	return sectionDescriptors, nil
}

// estimateSectionsForMatcher checks the matcher against the section AMQs to determine approximate section membership.
// This is an inexact lookup and only returns probable sections: there may be false positives, but no true negatives. There is no additional metadata returned beyond the section info.
func (m *ObjectMetastore) estimateSectionsForMatcher(ctx context.Context, indexObjects []*dataobj.Object, matcher *labels.Matcher) ([]SectionKey, error) {
	timer := prometheus.NewTimer(m.metrics.estimateSectionsTotalDuration)
	defer timer.ObserveDuration()

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(m.parallelism)

	sColumnName := scalar.NewStringScalar(matcher.Name)

	var matchedSections []SectionKey
	var matchedSectionsMu sync.Mutex
	for _, indexObject := range indexObjects {
		g.Go(func() error {
			pointerReadTimer := prometheus.NewTimer(m.metrics.estimateSectionsPointerReadDuration)
			var objMatchedSections []SectionKey
			err := forEachMatchedPointerSectionKey(ctx, indexObject, sColumnName, matcher.Value, func(sk SectionKey) {
				objMatchedSections = append(objMatchedSections, sk)
			})
			if err != nil {
				return fmt.Errorf("reading section keys: %w", err)
			}
			pointerReadTimer.ObserveDuration()

			if len(objMatchedSections) == 0 {
				// TODO(benclive): Find a way to differentiate between unknown columns and columns missing the target value.
				// For now, log a warning to track how often this happens.
				level.Warn(m.logger).Log("msg", "no section keys found for column")
			}

			matchedSectionsMu.Lock()
			matchedSections = append(matchedSections, objMatchedSections...)
			matchedSectionsMu.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	m.metrics.estimateSectionsSections.Observe(float64(len(matchedSections)))
	return matchedSections, nil
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
