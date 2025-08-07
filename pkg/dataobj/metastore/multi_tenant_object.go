package metastore

import (
	"bytes"
	"context"
	fmt "fmt"
	"maps"
	"slices"
	strings "strings"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/tenant"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/pointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

type MultiTenantObjectMetastore ObjectMetastore

func NewMultiTenantObjectMetastore(bucket objstore.Bucket, logger log.Logger, reg prometheus.Registerer) *MultiTenantObjectMetastore {
	store := &MultiTenantObjectMetastore{
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

func (m *MultiTenantObjectMetastore) Values(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) {
	values := map[string]struct{}{}

	err := m.forEachLabel(ctx, start, end, func(label labels.Label) {
		if _, ok := values[label.Value]; !ok {
			values[label.Value] = struct{}{}
		}
	}, matchers...)

	return slices.Collect(maps.Keys(values)), err
}

func (m *MultiTenantObjectMetastore) forEachLabel(ctx context.Context, start, end time.Time, foreach func(labels.Label), matchers ...*labels.Matcher) error {
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

func (m *MultiTenantObjectMetastore) Labels(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, error) {
	uniqueLabels := map[string]struct{}{}

	err := m.forEachLabel(ctx, start, end, func(label labels.Label) {
		if _, ok := uniqueLabels[label.Name]; !ok {
			uniqueLabels[label.Name] = struct{}{}
		}
	}, matchers...)

	return slices.Collect(maps.Keys(uniqueLabels)), err
}

func (m *MultiTenantObjectMetastore) DataObjects(ctx context.Context, start, end time.Time, _ ...*labels.Matcher) ([]string, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	level.Debug(m.logger).Log("msg", "ObjectMetastore.DataObjects", "tenant", tenantID, "start", start, "end", end)

	// Get all metastore paths for the time range
	var storePaths []string
	for path := range multiTenantIterStorePaths(start, end) {
		storePaths = append(storePaths, path)
	}

	// List objects from all stores concurrently
	return m.listObjectsFromStores(ctx, tenantID, storePaths, start, end)
}

func (m *MultiTenantObjectMetastore) Streams(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]*labels.Labels, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	level.Debug(m.logger).Log("msg", "ObjectMetastore.Streams", "tenant", tenantID, "start", start, "end", end, "matchers", matchersToString(matchers))

	// Get all metastore paths for the time range
	var storePaths []string
	for path := range multiTenantIterStorePaths(start, end) {
		storePaths = append(storePaths, path)
	}

	// List objects from all stores concurrently
	paths, err := m.listObjectsFromStores(ctx, tenantID, storePaths, start, end)
	if err != nil {
		return nil, err
	}

	// Search the stream sections of the matching objects to find matching streams
	predicate := streamPredicateFromMatchers(start, end, matchers...)
	return m.listStreamsFromObjects(ctx, tenantID, paths, predicate)
}

func (m *MultiTenantObjectMetastore) StreamIDs(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, [][]int64, []int, error) {
	return nil, nil, nil, errors.New("not implemented for this querier")
}

func (m *MultiTenantObjectMetastore) StreamIDsWithSections(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, [][]int64, [][]int, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	level.Debug(m.logger).Log("msg", "ObjectMetastore.StreamIDs", "tenant", tenantID, "start", start, "end", end, "matchers", matchersToString(matchers))

	// Get all metastore paths for the time range
	var storePaths []string
	for path := range multiTenantIterStorePaths(start, end) {
		storePaths = append(storePaths, path)
	}
	level.Debug(m.logger).Log("msg", "got metastore object paths", "tenant", tenantID, "paths", strings.Join(storePaths, ","))

	// List objects from all stores concurrently
	paths, err := m.listObjectsFromStores(ctx, tenantID, storePaths, start, end)
	level.Debug(m.logger).Log("msg", "got data object paths", "tenant", tenantID, "paths", strings.Join(paths, ","), "err", err)
	if err != nil {
		return nil, nil, nil, err
	}

	// Search the stream sections of the matching objects to find matching streams
	predicate := streamPredicateFromMatchers(start, end, matchers...)
	streamIDs, sections, err := m.listStreamIDsFromObjects(ctx, tenantID, paths, predicate)
	level.Debug(m.logger).Log("msg", "got streams and sections", "tenant", tenantID, "streams", len(streamIDs), "sections", len(sections), "err", err)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to list stream IDs and sections from objects: %w", err)
	}

	if len(streamIDs) == 0 {
		return []string{}, [][]int64{}, [][]int{}, nil
	}

	// Remove objects that do not contain any matching streams
	level.Debug(m.logger).Log("msg", "remove objects that do not contain any matching streams", "paths", len(paths), "streams", len(streamIDs), "sections", len(sections))
	for i := 0; i < len(paths); i++ {
		if len(streamIDs[i]) == 0 {
			level.Debug(m.logger).Log("msg", "remove object", "path", paths[i])
			paths = slices.Delete(paths, i, i+1)
			streamIDs = slices.Delete(streamIDs, i, i+1)
			sections = slices.Delete(sections, i, i+1)
			i--
		}
	}

	cnt := 0
	for _, section := range sections {
		cnt += len(section)
	}

	level.Debug(m.logger).Log("msg", "resolved unique sections", "paths", len(paths), "total_sections", cnt)

	return paths, streamIDs, sections, nil
}

func (m *MultiTenantObjectMetastore) Sections(ctx context.Context, start, end time.Time, matchers []*labels.Matcher, predicates []*labels.Matcher) ([]*DataobjSectionDescriptor, error) {
	sectionsTimer := prometheus.NewTimer(m.metrics.resolvedSectionsTotalDuration)

	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

	// Get all metastore paths for the time range
	var storePaths []string
	for path := range multiTenantIterStorePaths(start, end) {
		storePaths = append(storePaths, path)
	}

	// List objects from all stores concurrently
	paths, err := m.listObjectsFromStores(ctx, tenantID, storePaths, start, end)
	if err != nil {
		return nil, err
	}

	// Search the stream sections of the matching objects to find matching streams
	streamMatchers := streamPredicateFromMatchers(start, end, matchers...)
	pointerPredicate := pointers.TimeRangeRowPredicate{
		Start: start,
		End:   end,
	}
	streamSectionPointers, err := m.getSectionsForStreams(ctx, tenantID, paths, streamMatchers, pointerPredicate)
	if err != nil {
		return nil, err
	}
	initialSectionPointersCount := len(streamSectionPointers)

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

	duration := sectionsTimer.ObserveDuration()
	m.metrics.resolvedSectionsTotal.Observe(float64(len(streamSectionPointers)))
	m.metrics.resolvedSectionsRatio.Observe(float64(len(streamSectionPointers)) / float64(initialSectionPointersCount))
	level.Debug(m.logger).Log("msg", "resolved sections", "duration", duration, "sections", len(streamSectionPointers), "ratio", float64(len(streamSectionPointers))/float64(initialSectionPointersCount))

	return streamSectionPointers, nil
}

func (m *MultiTenantObjectMetastore) listObjects(ctx context.Context, tenant string, path string, start, end time.Time) ([]string, error) {
	var buf bytes.Buffer
	objectReader, err := m.bucket.Get(ctx, path)
	if err != nil {
		return nil, err
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
	err = forEachStream(ctx, tenant, object, nil, func(stream streams.Stream) {
		ok, objPath := objectOverlapsRange(stream.Labels, start, end)
		if ok {
			objectPaths = append(objectPaths, objPath)
		}
	}, true)
	if err != nil {
		return nil, err
	}

	/* 	// Then we iterate over index objects based on the new format.
	   	predicate := indexpointers.TimeRangeRowPredicate{
	   		Start: start.UTC(),
	   		End:   end.UTC(),
	   	}
	   	err = forEachIndexPointer(ctx, object, predicate, func(indexPointer indexpointers.IndexPointer) {
	   		objectPaths = append(objectPaths, indexPointer.Path)
	   	})
	   	if err != nil {
	   		return nil, err
	   	} */

	return objectPaths, nil
}

// listObjectsFromStores concurrently lists objects from multiple metastore files
func (m *MultiTenantObjectMetastore) listObjectsFromStores(ctx context.Context, tenant string, storePaths []string, start, end time.Time) ([]string, error) {
	objects := make([][]string, len(storePaths))
	g, ctx := errgroup.WithContext(ctx)

	for i, path := range storePaths {
		g.Go(func() error {
			var err error
			objects[i], err = m.listObjects(ctx, tenant, path, start, end)
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

func (m *MultiTenantObjectMetastore) listStreamsFromObjects(ctx context.Context, tenant string, paths []string, predicate streams.RowPredicate) ([]*labels.Labels, error) {
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

			return forEachStream(ctx, tenant, object, predicate, func(stream streams.Stream) {
				addLabels(&mu, foundStreams, &stream.Labels)
			}, true)
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

func (m *MultiTenantObjectMetastore) listStreamIDsFromObjects(ctx context.Context, tenant string, paths []string, predicate streams.RowPredicate) ([][]int64, [][]int, error) {
	streamIDs := make([][]int64, len(paths))
	sections := make([][]int, len(paths))

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(m.parallelism)

	for idx, path := range paths {
		g.Go(func() error {
			object, err := dataobj.FromBucket(ctx, m.bucket, path)
			if err != nil {
				return fmt.Errorf("getting object from bucket: %w", err)
			}

			sections[idx] = make([]int, 0)
			streamIDs[idx] = make([]int64, 0, 8)

			return forEachStreamWithSection(ctx, tenant, object, predicate, func(stream streams.Stream, sectionIdx int) {
				streamIDs[idx] = append(streamIDs[idx], stream.ID)
				sections[idx] = append(sections[idx], sectionIdx)
				slices.Sort(sections[idx])
				sections[idx] = slices.Compact(sections[idx])
			}, true)
		})
	}

	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	return streamIDs, sections, nil
}

// getSectionsForStreams reads the section data from matching streams and aggregates them into section descriptors.
// This is an exact lookup and includes metadata from the streams in each section: the stream IDs, the min-max timestamps, the number of bytes & number of lines.
func (m *MultiTenantObjectMetastore) getSectionsForStreams(ctx context.Context, tenant string, paths []string, streamPredicate streams.RowPredicate, timeRangePredicate pointers.TimeRangeRowPredicate) ([]*DataobjSectionDescriptor, error) {
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
			err = forEachStream(ctx, tenant, idxObject, streamPredicate, func(stream streams.Stream) {
				matchingStreamIDs = append(matchingStreamIDs, stream.ID)
			}, true)
			if err != nil {
				return fmt.Errorf("reading streams from index: %w", err)
			}
			streamReadTimer.ObserveDuration()

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
func (m *MultiTenantObjectMetastore) estimateSectionsForPredicates(ctx context.Context, paths []string, predicate pointers.RowPredicate) ([]*DataobjSectionDescriptor, error) {
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
