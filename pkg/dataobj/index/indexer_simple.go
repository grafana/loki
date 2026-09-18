package index

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

type simpleIndexer struct {
	calculator calculator
	logger     log.Logger
	idxBucket  objstore.Bucket
	tocWriter  *metastore.TableOfContentsWriter
	metrics    *simpleIndexerMetrics
}

// NewSimpleIndexer returns an indexer that builds one index object per data
// object it is given.
//
// The returned indexer is not safe for concurrent use: it accumulates state in
// the calculator and the Table of Contents writer, neither of which guards it.
func NewSimpleIndexer(calculator calculator, logger log.Logger, idxBucket objstore.Bucket, reg prometheus.Registerer) (*simpleIndexer, error) {
	tocWriter := metastore.NewTableOfContentsWriter(idxBucket, logger)
	if err := tocWriter.RegisterMetrics(reg); err != nil {
		return nil, fmt.Errorf("failed to register Table of Contents writer metrics: %w", err)
	}

	metrics := newSimpleIndexerMetrics()
	if err := metrics.register(reg); err != nil {
		return nil, fmt.Errorf("failed to register indexer metrics: %w", err)
	}

	return &simpleIndexer{
		calculator: calculator,
		logger:     logger,
		idxBucket:  idxBucket,
		tocWriter:  tocWriter,
		metrics:    metrics,
	}, nil
}

func (s *simpleIndexer) Index(ctx context.Context, obj *dataobj.Object, objPath string) error {
	objLogger := log.With(s.logger, "object_path", objPath)

	s.metrics.attempts.Inc()
	timer := prometheus.NewTimer(s.metrics.duration)
	defer timer.ObserveDuration()

	err := s.index(ctx, obj, objPath, objLogger)
	if err != nil {
		s.metrics.failures.Inc()
		s.calculator.Reset()
	}

	return err
}

func (s *simpleIndexer) index(ctx context.Context, obj *dataobj.Object, objPath string, objLogger log.Logger) error {
	err := s.calculator.Calculate(ctx, objLogger, obj, objPath)
	if err != nil {
		return fmt.Errorf("calculate object: %w", err)
	}

	idxObj, closer, tenantTimeRanges, err := s.calculator.Flush()
	if err != nil {
		if errors.Is(err, indexobj.ErrBuilderEmpty) {
			// Nothing was indexed, so there is no index object to upload and
			// nothing to record in the metastore ToC. The data object is
			// therefore not discoverable by queries, which is worth knowing
			// about.
			s.metrics.empty.Inc()
			level.Warn(objLogger).Log("msg", "no index was built for data object")
			return nil
		}
		return fmt.Errorf("failed to flush calculator: %w", err)
	}
	// The index object is fully consumed by the time this returns, so failing
	// to release its scratch storage must not fail the index.
	defer func() {
		if err := closer.Close(); err != nil {
			level.Warn(objLogger).Log("msg", "failed to release index object", "err", err)
		}
	}()

	idxObjKey, err := ObjectKey(ctx, idxObj)
	if err != nil {
		return fmt.Errorf("failed to generate index object key: %w", err)
	}

	idxReader, err := idxObj.Reader(ctx)
	if err != nil {
		return fmt.Errorf("failed to read index object: %w", err)
	}
	defer func() {
		if err := idxReader.Close(); err != nil {
			level.Warn(objLogger).Log("msg", "failed to close index object reader", "err", err)
		}
	}()

	if err := s.idxBucket.Upload(ctx, idxObjKey, idxReader); err != nil {
		return fmt.Errorf("failed to upload index object: %w", err)
	}

	fileSize := uint64(idxObj.Size())
	for i := range tenantTimeRanges {
		tenantTimeRanges[i].FileSize = fileSize
	}

	if err := s.tocWriter.WriteEntry(ctx, idxObjKey, tenantTimeRanges); err != nil {
		return fmt.Errorf("failed to update metastore ToC file: %w", err)
	}

	level.Debug(s.logger).Log("msg", "flushed index object", "objPath", objPath,
		"idxPath", idxObjKey, "idxSize", idxObj.Size(), "tenants", len(tenantTimeRanges))

	return nil
}
