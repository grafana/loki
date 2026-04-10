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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore/multitenancy"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

type logsIndexCalculation interface {
	// Name returns a short identifier for this calculation step, used for metrics labels.
	Name() string
	// Prepare is called before the first batch of logs is processed in order to initialize any state.
	Prepare(ctx context.Context, section *dataobj.Section, stats logs.Stats) error
	// ProcessBatch is called for each batch of logs records.
	// Implementations can assume to have exclusive access to the builder via the calculation context. They must not retain references to it after the call returns.
	ProcessBatch(ctx context.Context, context *logsCalculationContext, batch []logs.Record) error
	// Flush is called after all logs in a section have been processed.
	// Implementations can assume to have exclusive access to the builder via the calculation context. They must not retain references to it after the call returns.
	Flush(ctx context.Context, context *logsCalculationContext) error
}

type logsCalculationContext struct {
	tenantID       string
	objectPath     string
	sectionIdx     int64
	streamIDLookup map[int64]int64
	// TODO(twhitney): monitor the memory of this. [streamLabels] is passed in from Calculate,
	// and is thus object scoped. As longs as streams sections stay small enough this shouldn't
	// be a problem.
	streamLabels map[int64]labels.Labels // source stream ID -> labels
	builder      *indexobj.Builder
}

// These steps are applied to all logs and are unique to a section
func getLogsCalculationSteps() []logsIndexCalculation {
	return []logsIndexCalculation{
		&streamStatisticsCalculation{},
		&columnValuesCalculation{},
		&statsCalculation{sortSchemaKeys: defaultSortSchemaKeys},
		&labelPostingsCalculation{},
	}
}

// Calculator is used to calculate the indexes for a logs object and write them to the builder.
// It reads data from the logs object in order to build bloom filters and per-section stream metadata.
type Calculator struct {
	indexobjBuilder *indexobj.Builder
	builderMtx      sync.Mutex
	metrics         *calculatorMetrics
}

func NewCalculator(indexobjBuilder *indexobj.Builder) *Calculator {
	return &Calculator{
		indexobjBuilder: indexobjBuilder,
		metrics:         newCalculatorMetrics(),
	}
}

// RegisterMetrics registers the calculator's prometheus metrics with the given registerer.
func (c *Calculator) RegisterMetrics(reg prometheus.Registerer) error {
	return c.metrics.register(reg)
}

// UnregisterMetrics unregisters the calculator's prometheus metrics.
func (c *Calculator) UnregisterMetrics(reg prometheus.Registerer) {
	c.metrics.unregister(reg)
}

func (c *Calculator) Reset() {
	c.indexobjBuilder.Reset()
}

func (c *Calculator) TimeRanges() []multitenancy.TimeRange {
	return c.indexobjBuilder.TimeRanges()
}

func (c *Calculator) Flush() (*dataobj.Object, io.Closer, error) {
	return c.indexobjBuilder.Flush()
}

func (c *Calculator) IsFull() bool {
	return c.indexobjBuilder.IsFull()
}

// Calculate reads the log data from the input logs object and appends the resulting indexes to calculator's builder.
// Calculate is not thread-safe.
func (c *Calculator) Calculate(ctx context.Context, logger log.Logger, reader *dataobj.Object, objectPath string) error {
	g, streamsCtx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))
	streamIDLookupByTenant := sync.Map{}
	streamLabelsByTenant := sync.Map{}

	// Streams Section: process these first to ensure all streams have been added to the builder and are given new IDs.
	for i, section := range reader.Sections().Filter(streams.CheckSection) {
		g.Go(func() error {
			streamIDLookup := make(map[int64]int64)
			streamLabels, err := c.processStreamsSection(streamsCtx, section, streamIDLookup)
			if err != nil {
				return fmt.Errorf("failed to process stream section path=%s section=%d: %w", objectPath, i, err)
			}
			// This is safe as each data object has just one streams section per tenant, which means different sections cannot overwrite the results of each other.
			_, exists := streamIDLookupByTenant.LoadOrStore(section.Tenant, streamIDLookup)
			if exists {
				panic("multiple streams sections for the same tenant within one data object")
			}

			_, labelsExist := streamLabelsByTenant.LoadOrStore(section.Tenant, streamLabels)
			if labelsExist {
				panic("multiple streams sections for the same tenant within one data object")
			}
			return nil
		})
	}

	// Wait for the streams sections to be done.
	if err := g.Wait(); err != nil {
		return err
	}

	g, logsCtx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))
	// Logs Section: these can be processed in parallel once we have the stream IDs for the tenant.
	// TODO(benclive): Start processing logs sections as soon as the stream sections are done, tenant by tenant. That way we don't need to wait for the biggest stream sections before processing the logs.
	for i, section := range reader.Sections().Filter(logs.CheckSection) {
		g.Go(func() error {
			sectionLogger := log.With(logger, "section", i)
			streamIDLookup, ok := streamIDLookupByTenant.Load(section.Tenant)
			if !ok {
				return fmt.Errorf("stream ID lookup not found for tenant %s", section.Tenant)
			}
			streamLabelsVal, ok := streamLabelsByTenant.Load(section.Tenant)
			if !ok {
				return fmt.Errorf("stream labels not found for tenant %s", section.Tenant)
			}
			// 1. A bloom filter for each column in the logs section.
			// 2. A per-section stream time-range index using min/max of each stream in the logs section. StreamIDs will reference the aggregate stream section.
			if err := c.processLogsSection(logsCtx, sectionLogger, objectPath, section, int64(i), streamIDLookup.(map[int64]int64), streamLabelsVal.(map[int64]labels.Labels)); err != nil {
				return fmt.Errorf("failed to process logs section path=%s section=%d: %w", objectPath, i, err)
			}
			return nil
		})
	}

	// Wait for the logs sections to be done.
	if err := g.Wait(); err != nil {
		return err
	}
	return nil
}

func (c *Calculator) processStreamsSection(ctx context.Context, section *dataobj.Section, streamIDLookup map[int64]int64) (map[int64]labels.Labels, error) {
	streamSection, err := streams.Open(ctx, section)
	if err != nil {
		return nil, fmt.Errorf("failed to open stream section: %w", err)
	}

	rowReader := streams.NewRowReader(streamSection)
	defer rowReader.Close()

	if err := rowReader.Open(ctx); err != nil {
		return nil, fmt.Errorf("failed to open stream row reader: %w", err)
	}

	streamBuf := make([]streams.Stream, 8192)
	streamLabels := map[int64]labels.Labels{}
	for {
		n, err := rowReader.Read(ctx, streamBuf)
		if err != nil && !errors.Is(err, io.EOF) {
			return nil, fmt.Errorf("failed to read stream section: %w", err)
		}
		if n == 0 && errors.Is(err, io.EOF) {
			break
		}
		err = func() error {
			c.builderMtx.Lock()
			defer c.builderMtx.Unlock()
			for _, stream := range streamBuf[:n] {
				newStreamID, err := c.indexobjBuilder.AppendStream(section.Tenant, stream)
				if err != nil {
					return fmt.Errorf("failed to append to stream: %w", err)
				}
				streamIDLookup[stream.ID] = newStreamID
				streamLabels[stream.ID] = stream.Labels
			}
			return nil
		}()
		if err != nil {
			return nil, err
		}
	}
	return streamLabels, nil
}

// processLogsSection reads information from the logs section in order to build index information in the c.indexobjBuilder.
func (c *Calculator) processLogsSection(ctx context.Context, sectionLogger log.Logger, objectPath string, section *dataobj.Section, sectionIdx int64, streamIDLookup map[int64]int64, streamLabels map[int64]labels.Labels) error {
	logsBuf := make([]logs.Record, 8192)

	logsSection, err := logs.Open(ctx, section)
	if err != nil {
		return fmt.Errorf("failed to open logs section: %w", err)
	}

	tenantID := section.Tenant

	// Fetch the column statistics in order to init the bloom filters for each column
	stats, err := logs.ReadStats(ctx, logsSection)
	if err != nil {
		return fmt.Errorf("failed to read log section stats: %w", err)
	}

	calculationContext := &logsCalculationContext{
		tenantID:       tenantID,
		objectPath:     objectPath,
		sectionIdx:     sectionIdx,
		streamIDLookup: streamIDLookup,
		streamLabels:   streamLabels,
		builder:        c.indexobjBuilder,
	}

	calculationSteps := getLogsCalculationSteps()

	// Track cumulative duration per calculation step across all batches + flush.
	stepDurations := make([]time.Duration, len(calculationSteps))

	for _, calculation := range calculationSteps {
		if err := calculation.Prepare(ctx, section, stats); err != nil {
			return fmt.Errorf("failed to prepare calculation: %w", err)
		}
	}

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
		c.builderMtx.Lock()
		for i, calculation := range calculationSteps {
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
