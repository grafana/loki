package index

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/scratch"
)

// A Result describes the index object built and uploaded for a single data object.
type Result struct {
	Path       string
	TimeRanges []multitenancy.TimeRange
}

// A SimpleIndexer builds an index for a data object and uploads it.
// Safe for concurrent use.
type SimpleIndexer struct {
	cfg                    logsobj.BuilderBaseConfig
	scratchStore           scratch.Store
	logger                 log.Logger
	idxBucket              objstore.Bucket
	metrics                *IndexerMetrics
	indexObjBuilderMetrics *indexobj.BuilderMetrics
	calculatorMetrics      *CalculatorMetrics
}

// NewSimpleIndexer returns a new [SimpleIndexer].
func NewSimpleIndexer(
	cfg logsobj.BuilderBaseConfig,
	scratchStore scratch.Store,
	logger log.Logger,
	idxBucket objstore.Bucket,
	metrics *IndexerMetrics,
	indexObjBuilderMetrics *indexobj.BuilderMetrics,
	calculatorMetrics *CalculatorMetrics,
) (*SimpleIndexer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	return &SimpleIndexer{
		cfg:                    cfg,
		scratchStore:           scratchStore,
		logger:                 logger,
		idxBucket:              idxBucket,
		metrics:                metrics,
		indexObjBuilderMetrics: indexObjBuilderMetrics,
		calculatorMetrics:      calculatorMetrics,
	}, nil
}

// Index builds and uploads the index for obj, which is stored at objPath.
func (s *SimpleIndexer) Index(ctx context.Context, obj *dataobj.Object, objPath string) (res Result, err error) {
	objLogger := log.With(s.logger, "object_path", objPath)

	start := time.Now()
	defer func() { s.metrics.observeIndex(time.Since(start), err) }()

	return s.index(ctx, obj, objPath, objLogger)
}

// release closes a resource the index build no longer needs. Failures are
// counted and logged rather than returned: by the time anything is released
// the index has been uploaded, so a cleanup failure must not fail it.
func (s *SimpleIndexer) release(closer io.Closer, logger log.Logger, what string) {
	if err := closer.Close(); err != nil {
		s.metrics.releaseFailures.Inc()
		level.Warn(logger).Log("msg", "failed to release index build resource", "resource", what, "err", err)
	}
}

func (s *SimpleIndexer) index(ctx context.Context, obj *dataobj.Object, objPath string, objLogger log.Logger) (Result, error) {
	builder, err := indexobj.NewBuilder(s.cfg, s.scratchStore, s.indexObjBuilderMetrics)
	if err != nil {
		return Result{}, fmt.Errorf("failed to create index object builder: %w", err)
	}
	defer builder.Reset()

	calc := NewCalculator(builder, s.calculatorMetrics)
	defer calc.Reset()

	if err := calc.Calculate(ctx, objLogger, obj, objPath); err != nil {
		return Result{}, fmt.Errorf("calculate object: %w", err)
	}

	idxObj, closer, tenantTimeRanges, err := calc.Flush()
	if err != nil {
		return Result{}, fmt.Errorf("failed to flush calculator: %w", err)
	}
	defer s.release(closer, objLogger, "index object")

	idxObjKey, err := ObjectKey(ctx, idxObj)
	if err != nil {
		return Result{}, fmt.Errorf("failed to generate index object key: %w", err)
	}

	idxReader, err := idxObj.Reader(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("failed to read index object: %w", err)
	}
	defer s.release(idxReader, objLogger, "index object reader")

	if err := s.idxBucket.Upload(ctx, idxObjKey, idxReader); err != nil {
		return Result{}, fmt.Errorf("failed to upload index object: %w", err)
	}

	fileSize := uint64(idxObj.Size())
	for i := range tenantTimeRanges {
		tenantTimeRanges[i].FileSize = fileSize
	}

	level.Debug(objLogger).Log("msg", "uploaded index object",
		"idxPath", idxObjKey, "idxSize", fileSize, "tenants", len(tenantTimeRanges))

	return Result{Path: idxObjKey, TimeRanges: tenantTimeRanges}, nil
}
