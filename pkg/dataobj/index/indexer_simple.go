package index

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
)

// A SimpleIndexer builds one index object per data object it is given.
//
// SimpleIndexer is not safe for concurrent use: it accumulates state in the
// calculator and the Table of Contents writer, neither of which guards it.
type SimpleIndexer struct {
	calculator calculator
	logger     log.Logger
	idxBucket  objstore.Bucket
	tocWriter  *metastore.TableOfContentsWriter
	metrics    *indexerMetrics
}

// NewSimpleIndexer returns a new [SimpleIndexer], registering its metrics and
// those of the Table of Contents writer it owns with reg.
func NewSimpleIndexer(calculator calculator, logger log.Logger, idxBucket objstore.Bucket, reg prometheus.Registerer) (*SimpleIndexer, error) {
	tocWriter := metastore.NewTableOfContentsWriter(idxBucket, logger)
	if err := tocWriter.RegisterMetrics(reg); err != nil {
		return nil, fmt.Errorf("failed to register Table of Contents writer metrics: %w", err)
	}

	metrics := newIndexerMetrics()
	if err := metrics.register(reg); err != nil {
		return nil, fmt.Errorf("failed to register indexer metrics: %w", err)
	}

	return &SimpleIndexer{
		calculator: calculator,
		logger:     logger,
		idxBucket:  idxBucket,
		tocWriter:  tocWriter,
		metrics:    metrics,
	}, nil
}

func (s *SimpleIndexer) Index(ctx context.Context, obj *dataobj.Object, objPath string) error {
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

// release closes something the index build no longer needs. Failures are
// counted and logged rather than returned: by the time anything is released the
// index has been uploaded and recorded, so a cleanup failure must not fail it.
func (s *SimpleIndexer) release(closer io.Closer, logger log.Logger, what string) {
	if err := closer.Close(); err != nil {
		s.metrics.releaseFailures.Inc()
		level.Warn(logger).Log("msg", "failed to release index build resource", "resource", what, "err", err)
	}
}

func (s *SimpleIndexer) index(ctx context.Context, obj *dataobj.Object, objPath string, objLogger log.Logger) error {
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
	defer s.release(closer, objLogger, "index object")

	idxObjKey, err := ObjectKey(ctx, idxObj)
	if err != nil {
		return fmt.Errorf("failed to generate index object key: %w", err)
	}

	idxReader, err := idxObj.Reader(ctx)
	if err != nil {
		return fmt.Errorf("failed to read index object: %w", err)
	}
	defer s.release(idxReader, objLogger, "index object reader")

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
