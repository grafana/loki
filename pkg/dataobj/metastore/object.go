package metastore

import (
	"bytes"
	"context"
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
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

const (
	metastoreWindowSize = 12 * time.Hour
)

type ObjectMetastore struct {
	bucket      objstore.Bucket
	parallelism int
	logger      log.Logger
}

func metastorePath(tenantID string, window time.Time) string {
	return fmt.Sprintf("tenant-%s/metastore/%s.store", tenantID, window.Format(time.RFC3339))
}

func iterStorePaths(tenantID string, start, end time.Time) iter.Seq[string] {
	minMetastoreWindow := start.Truncate(metastoreWindowSize).UTC()
	maxMetastoreWindow := end.Truncate(metastoreWindowSize).UTC()

	return func(yield func(t string) bool) {
		for metastoreWindow := minMetastoreWindow; !metastoreWindow.After(maxMetastoreWindow); metastoreWindow = metastoreWindow.Add(metastoreWindowSize) {
			if !yield(metastorePath(tenantID, metastoreWindow)) {
				return
			}
		}
	}
}

func NewObjectMetastore(bucket objstore.Bucket, logger log.Logger) *ObjectMetastore {
	return &ObjectMetastore{
		bucket:      bucket,
		parallelism: 64,
		logger:      logger,
	}
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

func (m *ObjectMetastore) Streams(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]*labels.Labels, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	level.Debug(m.logger).Log("msg", "ObjectMetastore.Streams", "tenant", tenantID, "start", start, "end", end, "matchers", matchersToString(matchers))

	// Get all metastore paths for the time range
	var storePaths []string
	for path := range iterStorePaths(tenantID, start, end) {
		storePaths = append(storePaths, path)
	}

	// List objects from all stores concurrently
	paths, err := m.listObjectsFromStores(ctx, storePaths, start, end)
	if err != nil {
		return nil, err
	}

	// Search the stream sections of the matching objects to find matching streams
	predicate := predicateFromMatchers(start, end, matchers...)
	return m.listStreamsFromObjects(ctx, paths, predicate)
}

func (m *ObjectMetastore) StreamIDs(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]string, [][]int64, []int, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, nil, nil, err
	}
	level.Debug(m.logger).Log("msg", "ObjectMetastore.StreamIDs", "tenant", tenantID, "start", start, "end", end, "matchers", matchersToString(matchers))

	// Get all metastore paths for the time range
	var storePaths []string
	for path := range iterStorePaths(tenantID, start, end) {
		storePaths = append(storePaths, path)
	}
	level.Debug(m.logger).Log("msg", "got metastore object paths", "tenant", tenantID, "paths", strings.Join(storePaths, ","))

	// List objects from all stores concurrently
	paths, err := m.listObjectsFromStores(ctx, storePaths, start, end)
	level.Debug(m.logger).Log("msg", "got data object paths", "tenant", tenantID, "paths", strings.Join(storePaths, ","), "err", err)
	if err != nil {
		return nil, nil, nil, err
	}

	// Search the stream sections of the matching objects to find matching streams
	predicate := predicateFromMatchers(start, end, matchers...)
	streamIDs, sections, err := m.listStreamIDsFromObjects(ctx, paths, predicate)
	level.Debug(m.logger).Log("msg", "got streams and sections", "tenant", tenantID, "streams", len(streamIDs), "sections", len(sections), "err", err)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to list stream IDs and sections from objects: %w", err)
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

	return paths, streamIDs, sections, nil
}

func (m *ObjectMetastore) DataObjects(ctx context.Context, start, end time.Time, _ ...*labels.Matcher) ([]string, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
	level.Debug(m.logger).Log("msg", "ObjectMetastore.DataObjects", "tenant", tenantID, "start", start, "end", end)

	// Get all metastore paths for the time range
	var storePaths []string
	for path := range iterStorePaths(tenantID, start, end) {
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

		for _, streamLabel := range *streamLabels {
			foreach(streamLabel)
		}
	}

	return nil
}

func predicateFromMatchers(start, end time.Time, matchers ...*labels.Matcher) streams.RowPredicate {
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

	current := streams.AndRowPredicate{
		Left: predicates[0],
	}

	for _, predicate := range predicates[1:] {
		and := streams.AndRowPredicate{
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

	for i, path := range paths {
		func(idx int) {
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
		}(i)
	}

	if err := g.Wait(); err != nil {
		return nil, nil, err
	}

	return streamIDs, sections, nil
}

func addLabels(mtx *sync.Mutex, streams map[uint64][]*labels.Labels, newLabels *labels.Labels) {
	mtx.Lock()
	defer mtx.Unlock()

	sort.Sort(newLabels)

	key := newLabels.Hash()
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

	for _, section := range object.Sections() {
		if !indexpointers.CheckSection(section) {
			continue
		}

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
			if err != nil && err != io.EOF {
				return err
			}
			if num == 0 && err == io.EOF {
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
			if err != nil && err != io.EOF {
				return err
			}
			if num == 0 && err == io.EOF {
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

// objectOverlapsRange checks if an object's time range overlaps with the query range
func objectOverlapsRange(lbs labels.Labels, start, end time.Time) (bool, string) {
	var (
		objStart, objEnd time.Time
		objPath          string
	)
	for _, lb := range lbs {
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
	}
	if objStart.IsZero() || objEnd.IsZero() {
		return false, ""
	}
	if objEnd.Before(start) || objStart.After(end) {
		return false, ""
	}
	return true, objPath
}
