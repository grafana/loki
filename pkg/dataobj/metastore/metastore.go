package metastore

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"iter"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/grafana/dskit/backoff"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/thanos-io/objstore"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/logproto"
)

const (
	metastoreWindowSize = 12 * time.Hour
)

// Define our own builder config because metastore objects are significantly smaller.
var metastoreBuilderCfg = dataobj.BuilderConfig{
	TargetObjectSize:  32 * 1024 * 1024,
	TargetPageSize:    4 * 1024 * 1024,
	BufferSize:        32 * 1024 * 1024, // 8x page size
	TargetSectionSize: 4 * 1024 * 1024,  // object size / 8
}

type Manager struct {
	metastoreBuilder *dataobj.Builder
	tenantID         string
	metrics          *metastoreMetrics
	bucket           objstore.Bucket
	logger           log.Logger
	backoff          *backoff.Backoff
	buf              *bytes.Buffer

	builderOnce sync.Once
}

func NewManager(bucket objstore.Bucket, tenantID string, logger log.Logger) *Manager {
	metrics := newMetastoreMetrics()

	return &Manager{
		bucket:   bucket,
		metrics:  metrics,
		logger:   logger,
		tenantID: tenantID,
		backoff: backoff.New(context.TODO(), backoff.Config{
			MinBackoff: 50 * time.Millisecond,
			MaxBackoff: 10 * time.Second,
		}),
		builderOnce: sync.Once{},
	}
}

func (m *Manager) RegisterMetrics(reg prometheus.Registerer) error {
	return m.metrics.register(reg)
}

func (m *Manager) UnregisterMetrics(reg prometheus.Registerer) {
	m.metrics.unregister(reg)
}

func (m *Manager) initBuilder() error {
	var initErr error
	m.builderOnce.Do(func() {
		metastoreBuilder, err := dataobj.NewBuilder(metastoreBuilderCfg)
		if err != nil {
			initErr = err
			return
		}
		m.buf = bytes.NewBuffer(make([]byte, 0, metastoreBuilderCfg.TargetObjectSize))
		m.metastoreBuilder = metastoreBuilder
	})
	return initErr
}

// UpdateMetastore adds provided dataobj path to the metastore. Flush stats are used to determine the stored metadata about this dataobj.
func (m *Manager) UpdateMetastore(ctx context.Context, dataobjPath string, flushStats dataobj.FlushStats) error {
	var err error
	processingTime := prometheus.NewTimer(m.metrics.metastoreProcessingTime)
	defer processingTime.ObserveDuration()

	// Initialize builder if this is the first call for this partition
	if err := m.initBuilder(); err != nil {
		return err
	}

	minTimestamp, maxTimestamp := flushStats.MinTimestamp, flushStats.MaxTimestamp

	// Work our way through the metastore objects window by window, updating & creating them as needed.
	// Each one handles its own retries in order to keep making progress in the event of a failure.
	for metastorePath := range Iter(m.tenantID, minTimestamp, maxTimestamp) {
		m.backoff.Reset()
		for m.backoff.Ongoing() {
			err = m.bucket.GetAndReplace(ctx, metastorePath, func(existing io.Reader) (io.Reader, error) {
				m.buf.Reset()
				if existing != nil {
					level.Debug(m.logger).Log("msg", "found existing metastore, updating", "path", metastorePath)
					_, err := io.Copy(m.buf, existing)
					if err != nil {
						return nil, errors.Wrap(err, "copying to local buffer")
					}
				} else {
					level.Debug(m.logger).Log("msg", "no existing metastore found, creating new one", "path", metastorePath)
				}

				m.metastoreBuilder.Reset()

				if m.buf.Len() > 0 {
					replayDuration := prometheus.NewTimer(m.metrics.metastoreReplayTime)
					object := dataobj.FromReaderAt(bytes.NewReader(m.buf.Bytes()), int64(m.buf.Len()))
					if err := m.readFromExisting(ctx, object); err != nil {
						return nil, errors.Wrap(err, "reading existing metastore version")
					}
					replayDuration.ObserveDuration()
				}

				encodingDuration := prometheus.NewTimer(m.metrics.metastoreEncodingTime)

				ls := fmt.Sprintf("{__start__=\"%d\", __end__=\"%d\", __path__=\"%s\"}", minTimestamp.UnixNano(), maxTimestamp.UnixNano(), dataobjPath)
				err := m.metastoreBuilder.Append(logproto.Stream{
					Labels:  ls,
					Entries: []logproto.Entry{{Line: ""}},
				})
				if err != nil {
					return nil, errors.Wrap(err, "appending internal metadata stream")
				}

				m.buf.Reset()
				_, err = m.metastoreBuilder.Flush(m.buf)
				if err != nil {
					return nil, errors.Wrap(err, "flushing metastore builder")
				}
				encodingDuration.ObserveDuration()
				return m.buf, nil
			})
			if err == nil {
				level.Info(m.logger).Log("msg", "successfully merged & updated metastore", "metastore", metastorePath)
				m.metrics.incMetastoreWrites(statusSuccess)
				break
			}
			level.Error(m.logger).Log("msg", "failed to get and replace metastore object", "err", err, "metastore", metastorePath)
			m.metrics.incMetastoreWrites(statusFailure)
			m.backoff.Wait()
		}
		// Reset at the end too so we don't leave our memory hanging around between calls.
		m.metastoreBuilder.Reset()
	}
	return err
}

// readFromExisting reads the provided metastore object and appends the streams to the builder so it can be later modified.
func (m *Manager) readFromExisting(ctx context.Context, object *dataobj.Object) error {
	// Fetch sections
	si, err := object.Metadata(ctx)
	if err != nil {
		return errors.Wrap(err, "resolving object metadata")
	}

	// Read streams from existing metastore object and write them to the builder for the new object
	streams := make([]dataobj.Stream, 100)
	for i := 0; i < si.StreamsSections; i++ {
		streamsReader := dataobj.NewStreamsReader(object, i)
		for n, err := streamsReader.Read(ctx, streams); n > 0; n, err = streamsReader.Read(ctx, streams) {
			if err != nil && err != io.EOF {
				return errors.Wrap(err, "reading streams")
			}
			for _, stream := range streams[:n] {
				err = m.metastoreBuilder.Append(logproto.Stream{
					Labels:  stream.Labels.String(),
					Entries: []logproto.Entry{{Line: ""}},
				})
				if err != nil {
					return errors.Wrap(err, "appending streams")
				}
			}
		}
	}
	return nil
}

func metastorePath(tenantID string, window time.Time) string {
	return fmt.Sprintf("tenant-%s/metastore/%s.store", tenantID, window.Format(time.RFC3339))
}

func Iter(tenantID string, start, end time.Time) iter.Seq[string] {
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

// ListDataObjects returns a list of all dataobj paths for the given tenant and time range.
func ListDataObjects(ctx context.Context, bucket objstore.Bucket, tenantID string, start, end time.Time) ([]string, error) {
	// Get all metastore paths for the time range
	var storePaths []string
	for path := range Iter(tenantID, start, end) {
		storePaths = append(storePaths, path)
	}

	// List objects from all stores concurrently
	paths, err := listObjectsFromStores(ctx, bucket, storePaths, start, end)
	if err != nil {
		return nil, err
	}

	return paths, nil
}

// listObjectsFromStores concurrently lists objects from multiple metastore files
func listObjectsFromStores(ctx context.Context, bucket objstore.Bucket, storePaths []string, start, end time.Time) ([]string, error) {
	objects := make([][]string, len(storePaths))
	g, ctx := errgroup.WithContext(ctx)

	for i, path := range storePaths {
		g.Go(func() error {
			var err error
			objects[i], err = listObjects(ctx, bucket, path, start, end)
			// If the metastore object is not found, it means it's outside of any existing window
			// and we can safely ignore it.
			if err != nil && !bucket.IsObjNotFoundErr(err) {
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

func listObjects(ctx context.Context, bucket objstore.Bucket, path string, start, end time.Time) ([]string, error) {
	var buf bytes.Buffer
	objectReader, err := bucket.Get(ctx, path)
	if err != nil {
		return nil, err
	}
	n, err := buf.ReadFrom(objectReader)
	if err != nil {
		return nil, fmt.Errorf("reading metastore object: %w", err)
	}
	object := dataobj.FromReaderAt(bytes.NewReader(buf.Bytes()), n)
	si, err := object.Metadata(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolving object metadata: %w", err)
	}

	var objectPaths []string
	streams := make([]dataobj.Stream, 1024)
	for i := 0; i < si.StreamsSections; i++ {
		streamsReader := dataobj.NewStreamsReader(object, i)
		for {
			n, err := streamsReader.Read(ctx, streams)
			if err != nil && err != io.EOF {
				return nil, fmt.Errorf("reading streams: %w", err)
			}
			if n == 0 {
				break
			}
			for _, stream := range streams[:n] {
				ok, objPath := objectOverlapsRange(stream.Labels, start, end)
				if ok {
					objectPaths = append(objectPaths, objPath)
				}
			}
		}
	}
	return objectPaths, nil
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
