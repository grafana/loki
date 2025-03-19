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
	"sync"
	"time"

	"github.com/grafana/dskit/tenant"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

const (
	metastoreWindowSize = 12 * time.Hour
)

type ObjectMetastore struct {
	bucket objstore.Bucket
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

func NewObjectMetastore(bucket objstore.Bucket) *ObjectMetastore {
	return &ObjectMetastore{
		bucket: bucket,
	}
}

func (m *ObjectMetastore) Streams(ctx context.Context, start, end time.Time, matchers ...*labels.Matcher) ([]*labels.Labels, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}
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

func (m *ObjectMetastore) DataObjects(ctx context.Context, start, end time.Time, _ ...*labels.Matcher) ([]string, error) {
	tenantID, err := tenant.TenantID(ctx)
	if err != nil {
		return nil, err
	}

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

func predicateFromMatchers(start, end time.Time, matchers ...*labels.Matcher) dataobj.StreamsPredicate {
	if len(matchers) == 0 {
		return nil
	}

	predicates := make([]dataobj.StreamsPredicate, 0, len(matchers)+1)
	predicates = append(predicates, dataobj.TimeRangePredicate[dataobj.StreamsPredicate]{
		StartTime:    start,
		EndTime:      end,
		IncludeStart: true,
		IncludeEnd:   true,
	})
	for _, matcher := range matchers {
		switch matcher.Type {
		case labels.MatchEqual:
			predicates = append(predicates, dataobj.LabelMatcherPredicate{
				Name:  matcher.Name,
				Value: matcher.Value,
			})
		case labels.MatchNotEqual:
			predicates = append(predicates, dataobj.NotPredicate[dataobj.StreamsPredicate]{
				Inner: dataobj.LabelMatcherPredicate{
					Name:  matcher.Name,
					Value: matcher.Value,
				},
			})
		case labels.MatchRegexp:
			predicates = append(predicates, dataobj.LabelFilterPredicate{
				Name: matcher.Name,
				Keep: func(_, value string) bool {
					return matcher.Matches(value)
				},
			})
		case labels.MatchNotRegexp:
			predicates = append(predicates, dataobj.NotPredicate[dataobj.StreamsPredicate]{
				Inner: dataobj.LabelFilterPredicate{
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

	current := dataobj.AndPredicate[dataobj.StreamsPredicate]{
		Left: predicates[0],
	}

	for _, predicate := range predicates[1:] {
		and := dataobj.AndPredicate[dataobj.StreamsPredicate]{
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

func (m *ObjectMetastore) listStreamsFromObjects(ctx context.Context, paths []string, predicate dataobj.StreamsPredicate) ([]*labels.Labels, error) {
	mu := sync.Mutex{}
	streams := make(map[uint64][]*labels.Labels, 1024)

	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(64)

	for _, path := range paths {
		g.Go(func() error {
			object := dataobj.FromBucket(m.bucket, path)

			return forEachStream(ctx, object, predicate, func(stream dataobj.Stream) {
				addLabels(&mu, streams, &stream.Labels)
			})
		})
	}

	if err := g.Wait(); err != nil {
		return nil, err
	}

	streamsSlice := make([]*labels.Labels, 0, len(streams))
	for _, labels := range streams {
		streamsSlice = append(streamsSlice, labels...)
	}

	return streamsSlice, nil
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
	object := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), n)
	var objectPaths []string

	err = forEachStream(ctx, object, nil, func(stream dataobj.Stream) {
		ok, objPath := objectOverlapsRange(stream.Labels, start, end)
		if ok {
			objectPaths = append(objectPaths, objPath)
		}
	})
	if err != nil {
		return nil, err
	}
	return objectPaths, nil
}

func forEachStream(ctx context.Context, object *dataobj.Object, predicate dataobj.StreamsPredicate, f func(dataobj.Stream)) error {
	md, err := object.Metadata(ctx)
	if err != nil {
		return err
	}

	streams := make([]dataobj.Stream, 1024)
	for i := 0; i < md.StreamsSections; i++ {
		reader := dataobj.NewStreamsReader(object, i)
		if predicate != nil {
			err := reader.SetPredicate(predicate)
			if err != nil {
				return err
			}
		}
		for {
			num, err := reader.Read(ctx, streams)
			if err != nil && err != io.EOF {
				return err
			}
			if num == 0 && err == io.EOF {
				break
			}
			for _, stream := range streams[:num] {
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
		if lb.Name == "__start__" {
			tsNano, err := strconv.ParseInt(lb.Value, 10, 64)
			if err != nil {
				panic(err)
			}
			objStart = time.Unix(0, tsNano).UTC()
		}
		if lb.Name == "__end__" {
			tsNano, err := strconv.ParseInt(lb.Value, 10, 64)
			if err != nil {
				panic(err)
			}
			objEnd = time.Unix(0, tsNano).UTC()
		}
		if lb.Name == "__path__" {
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
