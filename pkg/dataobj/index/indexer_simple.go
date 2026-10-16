package index

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/thanos-io/objstore"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/scratch"
)

var (
	// ErrUnprocessableObject marks an error that the shape of a data object
	// causes. Retrying can't fix it.
	ErrUnprocessableObject = errors.New("unprocessable data object")
)

// A Result describes the index object built and uploaded for a single-tenant data object.
type Result struct {
	Path      string
	TimeRange dataobj.TimeRange
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
// obj must hold exactly one tenant.
func (s *SimpleIndexer) Index(ctx context.Context, obj *dataobj.Object, objPath string) (res Result, err error) {
	objLogger := log.With(s.logger, "object_path", objPath)

	start := time.Now()
	defer func() { s.metrics.observeIndex(time.Since(start), err) }()

	return s.index(ctx, obj, objPath, objLogger)
}

// release closes a closer, logs an error and increases releaseFailures if close fails.
func (s *SimpleIndexer) release(closer io.Closer, logger log.Logger, what string) {
	if err := closer.Close(); err != nil {
		s.metrics.releaseFailures.Inc()
		level.Warn(logger).Log("msg", "failed to release index build resource", "resource", what, "err", err)
	}
}

func (s *SimpleIndexer) index(ctx context.Context, obj *dataobj.Object, objPath string, objLogger log.Logger) (Result, error) {
	tenant, err := obj.Tenant()
	if err != nil {
		return Result{}, fmt.Errorf("%w: %w", ErrUnprocessableObject, err)
	}

	builder, err := indexobj.NewBuilder(tenant, s.cfg, s.scratchStore, s.indexObjBuilderMetrics)
	if err != nil {
		return Result{}, fmt.Errorf("failed to create index object builder: %w", err)
	}
	defer builder.Reset()

	calc := NewCalculator(builder, s.calculatorMetrics)
	defer calc.Reset()

	if err := calc.Calculate(ctx, objLogger, obj, objPath); err != nil {
		return Result{}, fmt.Errorf("calculate object: %w", err)
	}

	idxObj, closer, timeRange, err := calc.Flush()
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

	level.Debug(objLogger).Log("msg", "uploaded index object",
		"idxPath", idxObjKey, "idxSize", uint64(idxObj.Size()), "tenant", timeRange.Tenant)

	return Result{Path: idxObjKey, TimeRange: timeRange}, nil
}

// ObjectKey generates the object key for storing an index object in object storage.
func ObjectKey(ctx context.Context, object *dataobj.Object) (string, error) {
	h := sha256.New224()

	reader, err := object.Reader(ctx)
	if err != nil {
		return "", err
	}
	defer reader.Close()

	if _, err := io.Copy(h, reader); err != nil {
		return "", err
	}

	var sumBytes [sha256.Size224]byte
	sum := h.Sum(sumBytes[:0])
	sumStr := hex.EncodeToString(sum[:])

	return fmt.Sprintf("indexes/%s/%s", sumStr[:2], sumStr[2:]), nil
}
