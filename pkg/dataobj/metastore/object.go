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
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
	utillog "github.com/grafana/loki/v3/pkg/util/log"
)

const (
	metastoreWindowSize = 12 * time.Hour
)

type ObjectMetastore struct {
	cfg         StorageConfig
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

func storagePrefixFor(cfg StorageConfig, tenantID string) string {
	if cfg.IndexStoragePrefix == "" {
		return ""
	}
	if slices.Contains(cfg.EnabledTenantIDs, tenantID) {
		return cfg.IndexStoragePrefix
	}
	return ""
}

func metastorePath(tenantID string, window time.Time, prefix string) string {
	path := fmt.Sprintf("tenant-%s/metastore/%s.store", tenantID, window.Format(time.RFC3339))
	if prefix != "" {
		if !strings.HasSuffix(prefix, "/") {
			prefix += "/"
		}
		path = fmt.Sprintf("%s%s", prefix, path)
	}
	return path
}

func iterStorePaths(tenantID string, start, end time.Time, prefix string) iter.Seq[string] {
	minMetastoreWindow := start.Truncate(metastoreWindowSize).UTC()
	maxMetastoreWindow := end.Truncate(metastoreWindowSize).UTC()

	return func(yield func(t string) bool) {
		for metastoreWindow := minMetastoreWindow; !metastoreWindow.After(maxMetastoreWindow); metastoreWindow = metastoreWindow.Add(metastoreWindowSize) {
			if !yield(metastorePath(tenantID, metastoreWindow, prefix)) {
				return
			}
		}
	}
}

func NewObjectMetastore(cfg StorageConfig, bucket objstore.Bucket, logger log.Logger, reg prometheus.Registerer) *ObjectMetastore {
	store := &ObjectMetastore{
		cfg:         cfg,
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

func (m *ObjectMetastore) ResolveStrategy(tenants []string) ResolveStrategyType {
	if m.cfg.IndexStoragePrefix != "" {
		for _, tenant := range tenants {
			if !slices.Contains(m.cfg.EnabledTenantIDs, tenant) {
				return ResolveStrategyTypeDirect
			}
		}
		return ResolveStrategyTypeIndex
	}
	return ResolveStrategyTypeDirect
}

func (m *ObjectMetastore) Streams(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]*labels.Labels, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	level.Debug(utillog.WithContext(ctx, m.logger)).Log("msg", "ObjectMetastore.Streams", "tenant", tenantID, "start", start, "end", end, "matchers", matchersToString(matchers))

	// Get all metastore paths for the time range
	var (
		storePaths []string
		prefix     = storagePrefixFor(m.cfg, tenantID)
	)
	for path := range iterStorePaths(tenantID, start, end, prefix) {
		storePaths = append(storePaths, path)
	}

	// List objects from all stores concurrently
	paths, err := m.listObjectsFromStores(ctx, storePaths, start, end)
	if err != nil {
		return nil, err
	}

	// Search the stream sections of the matching objects to find matching streams
	predicate := streamPredicateFromMatchers(start, end, matchers...)
	return m.listStreamsFromObjects(ctx, paths, predicate)
}

func (m *ObjectMetastore) StreamIDs(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, [][]int64, []int, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, nil, nil, err
	}

	logger := utillog.WithContext(ctx, m.logger)
	level.Debug(logger).Log("msg", "ObjectMetastore.StreamIDs", "tenant", tenantID, "start", start, "end", end, "matchers", matchersToString(matchers))

	// Get all metastore paths for the time range
	var (
		storePaths []string
		prefix     = storagePrefixFor(m.cfg, tenantID)
	)
	for path := range iterStorePaths(tenantID, start, end, prefix) {
		storePaths = append(storePaths, path)
	}
	level.Debug(logger).Log("msg", "got metastore object paths", "tenant", tenantID, "paths", strings.Join(storePaths, ","))

	// List objects from all stores concurrently
	paths, err := m.listObjectsFromStores(ctx, storePaths, start, end)
	level.Debug(logger).Log("msg", "got data object paths", "tenant", tenantID, "paths", strings.Join(paths, ","), "err", err)
	if err != nil {
		return nil, nil, nil, err
	}

	// Search the stream sections of the matching objects to find matching streams
	predicate := streamPredicateFromMatchers(start, end, matchers...)
	streamIDs, sections, err := m.listStreamIDsFromObjects(ctx, paths, predicate)
	level.Debug(logger).Log("msg", "got streams and sections", "tenant", tenantID, "streams", len(streamIDs), "sections", len(sections), "err", err)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to list stream IDs and sections from objects: %w", err)
	}

	if len(streamIDs) == 0 {
		return []string{}, [][]int64{}, []int{}, nil
	}

	// Remove objects that do not contain any matching streams
	level.Debug(logger).Log("msg", "remove objects that do not contain any matching streams", "paths", len(paths), "streams", len(streamIDs), "sections", len(sections))
	for i := 0; i < len(paths); i++ {
		if len(streamIDs[i]) == 0 {
			level.Debug(logger).Log("msg", "remove object", "path", paths[i])
			paths = slices.Delete(paths, i, i+1)
			streamIDs = slices.Delete(streamIDs, i, i+1)
			sections = slices.Delete(sections, i, i+1)
			i--
		}
	}

	return paths, streamIDs, sections, nil
}

func (m *ObjectMetastore) Sections(ctx context.Context, start, end time.Time, matchers []*labels.Matcher, predicates []*labels.Matcher) ([]*DataobjSectionDescriptor, error) {
	sectionsTimer := prometheus.NewTimer(m.metrics.resolvedSectionsTotalDuration)

	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	// Get all metastore paths for the time range
	var (
		storePaths []string
		prefix     = storagePrefixFor(m.cfg, tenantID)
	)
	for path := range iterStorePaths(tenantID, start, end, prefix) {
		storePaths = append(storePaths, path)
	}

	level.Debug(m.logger).Log("msg", "got metastore object paths", "tenant", tenantID, "paths", strings.Join(storePaths, ","))

	// List objects from all stores concurrently
	paths, err := m.listObjectsFromStores(ctx, storePaths, start, end)
	if err != nil {
		return nil, err
	}

	// Search the stream sections of the matching objects to find matching streams
	streamMatchers := streamPredicateFromMatchers(start, end, matchers...)
	pointerPredicate := pointers.TimeRangeRowPredicate{
		Start: start,
		End:   end,
	}
	streamSectionPointers, err := m.getSectionsForStreams(ctx, paths, streamMatchers, pointerPredicate)
	if err != nil {
		return nil, err
	}
	initialSectionPointersCount := len(streamSectionPointers)

	if len(predicates) > 0 {
		// Search the section AMQs to estimate sections that might match the predicates
		// AMQs may return false positives so this is an over-estimate.
		pointerMatchers := pointerPredicateFromMatchers(predicates...)
		sectionMembershipEstimates, err := m.estimateSectionsForPredicates(ctx, paths, pointerMatchers)
		if err != nil {
			return nil, err
		}

		streamSectionPointers = intersectSections(streamSectionPointers, sectionMembershipEstimates)
		if len(streamSectionPointers) == 0 {
			return nil, errors.New("no relevant sections returned")
		}
	}

	duration := sectionsTimer.ObserveDuration()
	m.metrics.resolvedSectionsTotal.Observe(float64(len(streamSectionPointers)))
	m.metrics.resolvedSectionsRatio.Observe(float64(len(streamSectionPointers)) / float64(initialSectionPointersCount))
	level.Debug(utillog.WithContext(ctx, m.logger)).Log("msg", "resolved sections", "duration", duration, "sections", len(streamSectionPointers), "ratio", float64(len(streamSectionPointers))/float64(initialSectionPointersCount))

	return streamSectionPointers, nil
}

func intersectSections(sectionPointers []*DataobjSectionDescriptor, sectionMembershipEstimates []*DataobjSectionDescriptor) []*DataobjSectionDescriptor {
	existence := make(map[SectionKey]struct{}, len(sectionMembershipEstimates))
	for _, section := range sectionMembershipEstimates {
		existence[SectionKey{
			ObjectPath: section.ObjectPath,
			SectionIdx: section.SectionIdx,
		}] = struct{}{}
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
	var (
		storePaths []string
		prefix     = storagePrefixFor(m.cfg, tenantID)
	)
	for path := range iterStorePaths(tenantID, start, end, prefix) {
		storePaths = append(storePaths, path)
	}

	// List objects from all stores concurrently
	return m.listObjectsFromStores(ctx, storePaths, start, end)
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
	streams, err := m.Streams(ctx, start, end, matchers...)
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

func pointerPredicateFromMatchers(matchers ...*labels.Matcher) pointers.RowPredicate {
	if len(matchers) == 0 {
		return nil
	}

	predicates := make([]pointers.RowPredicate, 0, len(matchers)+1)
	for _, matcher := range matchers {
		switch matcher.Type {
		case labels.MatchEqual:
			predicates = append(predicates, pointers.BloomExistenceRowPredicate{
				Name:  matcher.Name,
				Value: matcher.Value,
			})
		}
	}

	current := predicates[0]

	for _, predicate := range predicates[1:] {
		and := pointers.AndRowPredicate{
			Left:  predicate,
			Right: current,
		}
		current = and
	}
	return current
}

// listObjectsFromStores concurrently lists objects from multiple metastore files
func (m *ObjectMetastore) listObjectsFromStores(ctx context.Context, storePaths []string, start, end time.Time) ([]string, error) {
	objects := make([][]string, len(storePaths))
	g, ctx := errgroup.WithContext(ctx)

	for i, path := range storePaths {
		g.Go(func() error {
			var err error
			objects[i], err = m.listObjects(ctx, path, start, end)
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

func (m *ObjectMetastore) listStreamIDsFromObjects(ctx context.Context, paths []string, predicate streams.RowPredicate) ([][]int64, []int, error) {
	streamIDs := make([][]int64, len(paths))
	sections := make([]int, len(paths))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(m.parallelism)

	for idx, path := range paths {
		g.Go(func() error {
			object, err := dataobj.FromBucket(ctx, m.bucket, path)
			if err != nil {
				return fmt.Errorf("getting object from bucket: %w", err)
			}

			sections[idx] = object.Sections().Count(logs.CheckSection)
			streamIDs[idx] = make([]int64, 0, 8)

			return forEachStream(ctx, object, predicate, func(stream streams.Stream) {
				streamIDs[idx] = append(streamIDs[idx], stream.ID)
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
func (m *ObjectMetastore) getSectionsForStreams(ctx context.Context, paths []string, streamPredicate streams.RowPredicate, timeRangePredicate pointers.TimeRangeRowPredicate) ([]*DataobjSectionDescriptor, error) {
	if streamPredicate == nil {
		// At least one stream matcher is required, currently.
		return nil, nil
	}

	timer := prometheus.NewTimer(m.metrics.streamFilterTotalDuration)
	defer timer.ObserveDuration()

	var sectionDescriptors []*DataobjSectionDescriptor

	sectionDescriptorsMutex := sync.Mutex{}
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(m.parallelism)

	for _, path := range paths {
		g.Go(func() error {
			var key SectionKey
			var matchingStreamIDs []int64

			idxObject, err := fetchObject(ctx, m.bucket, path)
			if err != nil {
				return fmt.Errorf("fetching object '%s' from bucket: %w", path, err)
			}

			streamReadTimer := prometheus.NewTimer(m.metrics.streamFilterStreamsReadDuration)
			err = forEachStream(ctx, idxObject, streamPredicate, func(stream streams.Stream) {
				matchingStreamIDs = append(matchingStreamIDs, stream.ID)
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
			err = forEachObjPointer(ctx, idxObject, timeRangePredicate, matchingStreamIDs, func(pointer pointers.SectionPointer) {
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

	m.metrics.streamFilterPaths.Observe(float64(len(paths)))
	m.metrics.streamFilterSections.Observe(float64(len(sectionDescriptors)))
	return sectionDescriptors, nil
}

// estimateSectionsForPredicates checks the predicates against the section AMQs to determine approximate section membership.
// This is an inexact lookup and only returns probable sections: there may be false positives, but no true negatives. There is no additional metadata returned beyond the section info.
func (m *ObjectMetastore) estimateSectionsForPredicates(ctx context.Context, paths []string, predicate pointers.RowPredicate) ([]*DataobjSectionDescriptor, error) {
	timer := prometheus.NewTimer(m.metrics.estimateSectionsTotalDuration)
	defer timer.ObserveDuration()

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(m.parallelism)

	var sectionDescriptors []*DataobjSectionDescriptor
	var sectionDescriptorsMutex sync.Mutex
	for _, path := range paths {
		g.Go(func() error {
			idxObject, err := fetchObject(ctx, m.bucket, path)
			if err != nil {
				return fmt.Errorf("fetching object from bucket: %w", err)
			}

			pointerReadTimer := prometheus.NewTimer(m.metrics.estimateSectionsPointerReadDuration)
			var objectSectionDescriptors []*DataobjSectionDescriptor
			err = forEachObjPointer(ctx, idxObject, predicate, nil, func(pointer pointers.SectionPointer) {
				objectSectionDescriptors = append(objectSectionDescriptors, &DataobjSectionDescriptor{
					SectionKey: SectionKey{
						ObjectPath: pointer.Path,
						SectionIdx: pointer.Section,
					},
				})
			})
			if err != nil {
				return fmt.Errorf("reading object from bucket: %w", err)
			}
			pointerReadTimer.ObserveDuration()

			sectionDescriptorsMutex.Lock()
			sectionDescriptors = append(sectionDescriptors, objectSectionDescriptors...)
			sectionDescriptorsMutex.Unlock()

			return nil
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	m.metrics.estimateSectionsPaths.Observe(float64(len(paths)))
	m.metrics.estimateSectionsSections.Observe(float64(len(sectionDescriptors)))
	return sectionDescriptors, nil
}

func fetchObject(ctx context.Context, bucket objstore.Bucket, path string) (*dataobj.Object, error) {
	return dataobj.FromBucket(ctx, bucket, path)
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

func (m *ObjectMetastore) listObjects(ctx context.Context, path string, start, end time.Time) ([]string, error) {
	var buf bytes.Buffer
	objectReader, err := m.bucket.Get(ctx, path)
	if err != nil {
		return nil, fmt.Errorf("getting metastore object: %w, path: %s", err, path)
	}
	n, err := buf.ReadFrom(objectReader)
	if err != nil {
		return nil, fmt.Errorf("reading metastore object: %w", err)
	}
	object, err := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), n)
	if err != nil {
		return nil, fmt.Errorf("getting object from reader: %w", err)
	}
	var objectPaths []string

	// First we iterate over index objects based on the old format.
	err = forEachStream(ctx, object, nil, func(stream streams.Stream) {
		ok, objPath := objectOverlapsRange(stream.Labels, start, end)
		if ok {
			objectPaths = append(objectPaths, objPath)
		}
	})
	if err != nil {
		return nil, err
	}

	// Then we iterate over index objects based on the new format.
	predicate := indexpointers.TimeRangeRowPredicate{
		Start: start.UTC(),
		End:   end.UTC(),
	}
	err = forEachIndexPointer(ctx, object, predicate, func(indexPointer indexpointers.IndexPointer) {
		objectPaths = append(objectPaths, indexPointer.Path)
	})
	if err != nil {
		return nil, err
	}

	return objectPaths, nil
}

func forEachIndexPointer(ctx context.Context, object *dataobj.Object, predicate indexpointers.RowPredicate, f func(indexpointers.IndexPointer)) error {
	var reader indexpointers.RowReader
	defer reader.Close()

	buf := make([]indexpointers.IndexPointer, 1024)

	for _, section := range object.Sections().Filter(indexpointers.CheckSection) {
		sec, err := indexpointers.Open(ctx, section)
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
			for _, indexPointer := range buf[:num] {
				f(indexPointer)
			}
		}
	}

	return nil
}

func forEachStream(ctx context.Context, object *dataobj.Object, predicate streams.RowPredicate, f func(streams.Stream)) error {
	var reader streams.RowReader
	defer reader.Close()

	buf := make([]streams.Stream, 1024)

	for _, section := range object.Sections() {
		if !streams.CheckSection(section) {
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

func forEachObjPointer(ctx context.Context, object *dataobj.Object, predicate pointers.RowPredicate, matchIDs []int64, f func(pointers.SectionPointer)) error {
	var reader pointers.RowReader
	defer reader.Close()

	buf := make([]pointers.SectionPointer, 1024)

	for _, section := range object.Sections().Filter(pointers.CheckSection) {
		sec, err := pointers.Open(ctx, section)
		if err != nil {
			return fmt.Errorf("opening section: %w", err)
		}

		reader.Reset(sec)
		err = reader.MatchStreams(slices.Values(matchIDs))
		if err != nil {
			return fmt.Errorf("matching streams: %w", err)
		}
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
			for _, pointer := range buf[:num] {
				f(pointer)
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

// objectOverlapsRange checks if an object's time range overlaps with the query range
func objectOverlapsRange(lbs labels.Labels, start, end time.Time) (bool, string) {
	var (
		objStart, objEnd time.Time
		objPath          string
	)

	lbs.Range(func(lb labels.Label) {
		if lb.Name == labelNameStart {
			tsNano, err := strconv.ParseInt(lb.Value, 10, 64)
			if err != nil {
				panic(err)
			}
			objStart = time.Unix(0, tsNano).UTC()
		}
		if lb.Name == labelNameEnd {
			tsNano, err := strconv.ParseInt(lb.Value, 10, 64)
			if err != nil {
				panic(err)
			}
			objEnd = time.Unix(0, tsNano).UTC()
		}
		if lb.Name == labelNamePath {
			objPath = lb.Value
		}
	})

	if objStart.IsZero() || objEnd.IsZero() {
		return false, ""
	}
	if objEnd.Before(start) || objStart.After(end) {
		return false, ""
	}
	return true, objPath
}
