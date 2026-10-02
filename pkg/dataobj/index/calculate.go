package index

import (
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"

	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

type logsIndexCalculation interface {
	// Name returns a short identifier for this calculation step, used for metrics labels.
	Name() string
	// Prepare is called before the first batch of logs is processed in order to initialize any state.
	Prepare(ctx context.Context, calcCtx *logsCalculationContext, section *dataobj.Section, stats logs.Stats) error
	// ProcessBatch is called for each batch of logs records.
	//
	// If ProcessBatchNeedsBuilderLock returns true, implementations can assume
	// to have exclusive access to the builder via the calculation context and
	// must not retain references to it after the call returns. If false, the
	// implementation MUST NOT touch the shared builder during ProcessBatch;
	// all shared-state mutation must be deferred to Flush.
	ProcessBatch(ctx context.Context, context *logsCalculationContext, batch []logs.Record) error
	// ProcessBatchNeedsBuilderLock reports whether ProcessBatch mutates the
	// shared builder. When false, ProcessBatch is safe to run concurrently
	// with other sections' lock-free ProcessBatch calls (each section has its
	// own calculation-step state, so there is no cross-section sharing).
	ProcessBatchNeedsBuilderLock() bool
	// Flush is called after all logs in a section have been processed.
	// Implementations can assume to have exclusive access to the builder via the calculation context. They must not retain references to it after the call returns.
	Flush(ctx context.Context, context *logsCalculationContext) error
}

type logsCalculationContext struct {
	objectPath     string
	sectionIdx     int64
	streamIDLookup map[int64]int64
	// TODO(twhitney): monitor the memory of this. [streamLabels] is passed in from Calculate,
	// and is thus object scoped. As longs as streams sections stay small enough this shouldn't
	// be a problem.
	streamLabels       map[int64]labels.Labels // source stream ID -> labels
	streamShardBuckets map[int64]uint32        // source stream ID -> shard bucket
	builder            *indexobj.Builder
}

// These steps are applied to all logs and are unique to a section
func getLogsCalculationSteps(sortSchema []string) []logsIndexCalculation {
	return []logsIndexCalculation{
		&streamStatisticsCalculation{},
		&columnValuesCalculation{},
		&statsCalculation{schema: sortSchema},
		&labelPostingsCalculation{},
	}
}

var (
	// ErrTenantMismatch is returned when a data object holds a section of a
	// tenant other than the [Calculator]'s tenant. It wraps
	// [ErrUnprocessableObject].
	ErrTenantMismatch = fmt.Errorf("%w: section belongs to another tenant", ErrUnprocessableObject)

	// ErrStreamsSectionCount is returned when a data object doesn't hold exactly
	// one streams section. It wraps [ErrUnprocessableObject].
	ErrStreamsSectionCount = fmt.Errorf("%w: data object must hold one streams section", ErrUnprocessableObject)
)

// Calculator is used to calculate the indexes for a logs object and write them to the builder.
// It reads data from the logs object in order to build bloom filters and per-section stream metadata.
//
// A Calculator is bound to the tenant of its builder. It indexes only data
// objects that hold that tenant alone.
type Calculator struct {
	indexobjBuilder  *indexobj.Builder
	builderMtx       sync.Mutex
	metrics          *CalculatorMetrics
	uncompressedSize uint64
}

// NewCalculator returns a [Calculator] for the tenant of indexobjBuilder.
func NewCalculator(indexobjBuilder *indexobj.Builder, metrics *CalculatorMetrics) *Calculator {
	return &Calculator{
		indexobjBuilder: indexobjBuilder,
		metrics:         metrics,
	}
}

func (c *Calculator) Reset() {
	c.indexobjBuilder.Reset()
	c.uncompressedSize = 0
}

// TimeRange returns the tenant, time range and uncompressed logs size of the
// data calculated since the last [Calculator.Flush] or [Calculator.Reset].
// MinTime and MaxTime are zero when no data was calculated.
func (c *Calculator) TimeRange() dataobj.TimeRange {
	timeRange := c.indexobjBuilder.TimeRange()
	timeRange.UncompressedLogsSize = c.uncompressedSize
	return timeRange
}

// Flush consumes the calculator's state and returns the built object together
// with the time range captured for it. The uncompressed-size accumulator is
// tied to the underlying builder's lifecycle: [indexobj.Builder.Flush] resets
// the builder, so we clear the accumulator in the same step to keep the two in
// sync. Otherwise a failed upload or ToC write followed by a Kafka retry would
// add the reprocessed bytes on top of the stale count.
func (c *Calculator) Flush() (*dataobj.Object, io.Closer, dataobj.TimeRange, error) {
	timeRange := c.TimeRange()

	obj, closer, err := c.indexobjBuilder.Flush()
	if err != nil {
		return nil, nil, dataobj.TimeRange{}, err
	}

	c.uncompressedSize = 0
	return obj, closer, timeRange, nil
}

func (c *Calculator) IsFull() bool {
	return c.indexobjBuilder.IsFull()
}

// Calculate reads the log data from the input logs object and appends the resulting indexes to calculator's builder.
// Calculate can index several objects into one index before [Calculator.Flush].
//
// Every section of the object must belong to the calculator's tenant, and the
// object must hold exactly one streams section. Otherwise Calculate returns
// [ErrTenantMismatch] or [ErrStreamsSectionCount] and does not change the
// builder. On any other error the builder holds part of the object, so the
// caller must reset or discard the calculator.
//
// Calculate is not thread-safe.
func (c *Calculator) Calculate(ctx context.Context, logger log.Logger, reader *dataobj.Object, objectPath string) error {
	streamsSection, err := c.validate(reader)
	if err != nil {
		return fmt.Errorf("path=%s: %w", objectPath, err)
	}

	// Process the streams section first, so that every stream has its new ID
	// in the builder before the logs sections refer to it.
	streamIDLookup := make(map[int64]int64)
	streamLabels, shardBuckets, err := c.processStreamsSection(ctx, streamsSection, streamIDLookup)
	if err != nil {
		return fmt.Errorf("failed to process stream section path=%s: %w", objectPath, err)
	}

	g, logsCtx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))
	for i, section := range reader.Sections().Filter(logs.CheckSection) {
		g.Go(func() error {
			sectionLogger := log.With(logger, "section", i)
			// 1. A bloom filter for each column in the logs section.
			// 2. A per-section stream time-range index using min/max of each stream in the logs section. StreamIDs will reference the aggregate stream section.
			if err := c.processLogsSection(logsCtx, sectionLogger, objectPath, section, int64(i), streamIDLookup, streamLabels, shardBuckets); err != nil {
				return fmt.Errorf("failed to process logs section path=%s section=%d: %w", objectPath, i, err)
			}
			return nil
		})
	}
	return g.Wait()
}

// validate checks that every section of reader belongs to the calculator's
// tenant, and that reader holds exactly one streams section. It returns that
// streams section.
func (c *Calculator) validate(reader *dataobj.Object) (*dataobj.Section, error) {
	var (
		tenant         = c.indexobjBuilder.Tenant()
		streamsSection *dataobj.Section
		streamsCount   int
		logsCount      int
	)
	for i, section := range reader.Sections() {
		if section.Tenant != tenant {
			return nil, fmt.Errorf("%w: section %d has tenant %q, want %q", ErrTenantMismatch, i, section.Tenant, tenant)
		}
		switch {
		case streams.CheckSection(section):
			streamsSection = section
			streamsCount++
		case logs.CheckSection(section):
			logsCount++
		}
	}

	if streamsCount != 1 {
		return nil, fmt.Errorf("%w: found %d streams sections and %d logs sections", ErrStreamsSectionCount, streamsCount, logsCount)
	}
	return streamsSection, nil
}

func (c *Calculator) processStreamsSection(ctx context.Context, section *dataobj.Section, streamIDLookup map[int64]int64) (map[int64]labels.Labels, map[int64]uint32, error) {
	streamSection, err := streams.Open(ctx, section)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to open stream section: %w", err)
	}

	rowReader := streams.NewRowReader(streamSection)
	defer rowReader.Close()

	if err := rowReader.Open(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to open stream row reader: %w", err)
	}

	streamBuf := make([]streams.Stream, 8192)
	streamLabels := make(map[int64]labels.Labels)
	shardBuckets := make(map[int64]uint32)
	for {
		n, err := rowReader.Read(ctx, streamBuf)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, nil, fmt.Errorf("failed to read stream section: %w", err)
		}
		if n == 0 && errors.Is(err, io.EOF) {
			break
		}
		for _, stream := range streamBuf[:n] {
			newStreamID, err := c.indexobjBuilder.AppendStream(stream)
			if err != nil {
				return nil, nil, fmt.Errorf("failed to append to stream: %w", err)
			}
			streamIDLookup[stream.ID] = newStreamID
			if _, ok := streamLabels[stream.ID]; !ok {
				streamLabels[stream.ID] = stream.Labels
				shardBuckets[stream.ID] = streams.ShardBucket(stream.Labels)
			}
			c.uncompressedSize += uint64(stream.UncompressedSize)
		}
	}
	return streamLabels, shardBuckets, nil
}

// processLogsSection reads information from the logs section in order to build index information in the c.indexobjBuilder.
// The provided section index counts only logs sections, matching the indexes yielded by Filter, not positions in reader.Sections().
func (c *Calculator) processLogsSection(ctx context.Context, sectionLogger log.Logger, objectPath string, section *dataobj.Section, sectionIdx int64, streamIDLookup map[int64]int64, streamLabels map[int64]labels.Labels, shardBuckets map[int64]uint32) error {
	logsBuf := make([]logs.Record, 8192)

	logsSection, err := logs.Open(ctx, section)
	if err != nil {
		return fmt.Errorf("failed to open logs section: %w", err)
	}

	schemaLabels, err := logsSection.SchemaLabels()
	if err != nil {
		return fmt.Errorf("failed to read logs section schema labels: %w", err)
	}

	// Fetch the column statistics in order to init the bloom filters for each column
	stats, err := logs.ReadStats(ctx, logsSection)
	if err != nil {
		return fmt.Errorf("failed to read log section stats: %w", err)
	}

	calculationContext := &logsCalculationContext{
		objectPath:         objectPath,
		sectionIdx:         sectionIdx,
		streamIDLookup:     streamIDLookup,
		streamLabels:       streamLabels,
		streamShardBuckets: shardBuckets,
		builder:            c.indexobjBuilder,
	}

	// Lock-free steps run without builderMtx held, so they must not touch the
	// shared builder. We pass them a context with builder=nil to turn any
	// accidental access into an immediate nil-pointer panic instead of a
	// silent data race.
	lockFreeContext := *calculationContext
	lockFreeContext.builder = nil

	calculationSteps := getLogsCalculationSteps(schemaLabels)

	// Track cumulative duration per calculation step across all batches + flush.
	stepDurations := make([]time.Duration, len(calculationSteps))

	// Lock the builder during Prepare because some calculations (e.g.,
	// columnValuesCalculation) mutate shared builder state via
	// PrepareBloomColumn. The Calculate method dispatches one goroutine per
	// logs section (see g.Go in Calculate), so processLogsSection calls for
	// the sections of one data object run concurrently against the same
	// postings builder.
	c.builderMtx.Lock()
	for _, calculation := range calculationSteps {
		if err := calculation.Prepare(ctx, calculationContext, section, stats); err != nil {
			c.builderMtx.Unlock()
			return fmt.Errorf("failed to prepare calculation: %w", err)
		}
	}
	c.builderMtx.Unlock()

	// TODO(benclive): Switch to a columnar reader instead of row based
	rowReader := logs.NewRowReader(logsSection)
	defer rowReader.Close()

	if err := rowReader.Open(ctx); err != nil {
		return fmt.Errorf("failed to open logs row reader: %w", err)
	}

	var cnt int
	for {
		n, err := rowReader.Read(ctx, logsBuf)
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed to read logs section: %w", err)
		}
		if n == 0 && errors.Is(err, io.EOF) {
			break
		}

		cnt += n

		// First pass: run lock-free steps. Each calculation-step instance is
		// owned by this section goroutine (see getLogsCalculationSteps above),
		// and these steps do not touch the shared builder until Flush, so no
		// locking is required here.
		for i, calculation := range calculationSteps {
			if calculation.ProcessBatchNeedsBuilderLock() {
				continue
			}
			start := time.Now()
			if err := calculation.ProcessBatch(ctx, &lockFreeContext, logsBuf[:n]); err != nil {
				return fmt.Errorf("failed to process batch: %w", err)
			}
			stepDurations[i] += time.Since(start)
		}

		if err := ctx.Err(); err != nil {
			return err
		}

		// Second pass: run steps that require exclusive access to the shared
		// builder under builderMtx.
		c.builderMtx.Lock()
		for i, calculation := range calculationSteps {
			if !calculation.ProcessBatchNeedsBuilderLock() {
				continue
			}
			start := time.Now()
			if err := calculation.ProcessBatch(ctx, calculationContext, logsBuf[:n]); err != nil {
				c.builderMtx.Unlock()
				return fmt.Errorf("failed to process batch: %w", err)
			}
			stepDurations[i] += time.Since(start)
		}
		c.builderMtx.Unlock()
	}

	c.builderMtx.Lock()
	for i, calculation := range calculationSteps {
		start := time.Now()
		if err := calculation.Flush(ctx, calculationContext); err != nil {
			c.builderMtx.Unlock()
			return fmt.Errorf("failed to flush calculation results: %w", err)
		}
		stepDurations[i] += time.Since(start)
	}
	c.builderMtx.Unlock()

	for i, calculation := range calculationSteps {
		c.metrics.observeStepDuration(calculation.Name(), stepDurations[i])
	}

	level.Info(sectionLogger).Log("msg", "finished processing logs section", "rowsProcessed", cnt)
	return nil
}
