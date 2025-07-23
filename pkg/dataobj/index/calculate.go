package index

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/go-kit/log"
	"github.com/go-kit/log/level"
	"github.com/prometheus/prometheus/model/labels"
	"golang.org/x/sync/errgroup"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// Calculator is used to calculate the indexes for a logs object and write them to the builder.
// It reads data from the logs object in order to build bloom filters and per-section stream metadata.
type Calculator struct {
	indexobjBuilder *indexobj.Builder
	builderMtx      sync.Mutex

	// indexStreamIDLookup is a mapping between the streamID in a logs object & a streamID in the index object.
	indexStreamIDLookup map[int64]int64
}

func NewCalculator(indexobjBuilder *indexobj.Builder) *Calculator {
	return &Calculator{indexobjBuilder: indexobjBuilder, builderMtx: sync.Mutex{}, indexStreamIDLookup: make(map[int64]int64)}
}

func (c *Calculator) Reset() {
	c.indexobjBuilder.Reset()
	clear(c.indexStreamIDLookup)
}

func (c *Calculator) Flush(buffer *bytes.Buffer) (indexobj.FlushStats, error) {
	return c.indexobjBuilder.Flush(buffer)
}

// Calculate reads the log data from the input logs object and appends the resulting indexes to calculator's builder.
// Calculate is not thread-safe.
func (c *Calculator) Calculate(ctx context.Context, logger log.Logger, reader *dataobj.Object, objectPath string) error {
	// The mapping must be unique for every logs object
	clear(c.indexStreamIDLookup)

	// Streams Section: process this section first to ensure all streams have been added to the builder and are given new IDs.
	for i, section := range reader.Sections().Filter(streams.CheckSection) {
		sectionLogger := log.With(logger, "section", i)
		if err := c.processStreamsSection(ctx, sectionLogger, section, objectPath); err != nil {
			return fmt.Errorf("failed to process stream section path=%s section=%d: %w", objectPath, i, err)
		}
	}

	// Logs Section: these can be processed in parallel once we have the stream IDs. This work is heavily CPU bound so is limited to GOMAXPROCS parallelism.
	g, ctx := errgroup.WithContext(ctx)
	g.SetLimit(runtime.GOMAXPROCS(0))
	for i, section := range reader.Sections().Filter(logs.CheckSection) {
		g.Go(func() error {
			sectionLogger := log.With(logger, "section", i)
			// 1. A bloom filter for each column in the logs section.
			// 2. A per-section stream time-range index using min/max of each stream in the logs section. StreamIDs will reference the aggregate stream section.
			if err := c.processLogsSection(ctx, sectionLogger, objectPath, section, int64(i)); err != nil {
				return fmt.Errorf("failed to process logs section path=%s section=%d: %w", objectPath, i, err)
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	return nil
}

func (c *Calculator) processStreamsSection(ctx context.Context, _ log.Logger, section *dataobj.Section, objectPath string) error {
	streamSection, err := streams.Open(ctx, section)
	if err != nil {
		return fmt.Errorf("failed to open stream section: %w", err)
	}

	streamBuf := make([]streams.Stream, 2048)
	rowReader := streams.NewRowReader(streamSection)
	for {
		n, err := rowReader.Read(ctx, streamBuf)
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed to read stream section: %w", err)
		}
		if n == 0 && errors.Is(err, io.EOF) {
			break
		}
		for _, stream := range streamBuf[:n] {
			newStreamID, err := c.indexobjBuilder.AppendStream(stream)
			if err != nil {
				return fmt.Errorf("failed to append to stream: %w", err)
			}
			c.indexStreamIDLookup[stream.ID] = newStreamID
		}
	}
	return nil
}

// processLogsSection reads information from the logs section in order to build index information in the c.indexobjBuilder.
func (c *Calculator) processLogsSection(ctx context.Context, sectionLogger log.Logger, objectPath string, section *dataobj.Section, sectionIdx int64) error {
	logsBuf := make([]logs.Record, 1024)
	type logInfo struct {
		objectPath string
		sectionIdx int64
		streamID   int64
		timestamp  time.Time
		length     int64
	}
	logsInfo := make([]logInfo, len(logsBuf))

	logsSection, err := logs.Open(ctx, section)
	if err != nil {
		return fmt.Errorf("failed to open logs section: %w", err)
	}

	// Fetch the column statistics in order to init the bloom filters for each column
	stats, err := logs.ReadStats(ctx, logsSection)
	if err != nil {
		return fmt.Errorf("failed to read log section stats: %w", err)
	}

	columnBloomBuilders := make(map[string]*bloom.BloomFilter)
	columnIndexes := make(map[string]int64)
	for _, column := range stats.Columns {
		if !logs.IsMetadataColumn(column.Type) {
			continue
		}
		columnBloomBuilders[column.Name] = bloom.NewWithEstimates(uint(column.Cardinality), 1.0/128.0)
		columnIndexes[column.Name] = column.ColumnIndex
	}

	// Read the whole logs section to extract all the column values.
	cnt := 0
	// TODO(benclive): Switch to a columnar reader instead of row based
	// This is also likely to be more performant, especially if we don't need to read the whole log line.
	// Note: the source object would need a new column storing just the length to avoid reading the log line itself.
	rowReader := logs.NewRowReader(logsSection)
	for {
		n, err := rowReader.Read(ctx, logsBuf)
		if err != nil && !errors.Is(err, io.EOF) {
			return fmt.Errorf("failed to read logs section: %w", err)
		}
		if n == 0 && errors.Is(err, io.EOF) {
			break
		}

		for i, log := range logsBuf[:n] {
			cnt++
			log.Metadata.Range(func(md labels.Label) {
				columnBloomBuilders[md.Name].Add([]byte(md.Value))
			})
			logsInfo[i].objectPath = objectPath
			logsInfo[i].sectionIdx = sectionIdx
			logsInfo[i].streamID = log.StreamID
			logsInfo[i].timestamp = log.Timestamp
			logsInfo[i].length = int64(len(log.Line))
		}

		// Lock the mutex once per read for perf reasons.
		c.builderMtx.Lock()
		for _, log := range logsInfo[:n] {
			err = c.indexobjBuilder.ObserveLogLine(log.objectPath, log.sectionIdx, log.streamID, c.indexStreamIDLookup[log.streamID], log.timestamp, log.length)
			if err != nil {
				c.builderMtx.Unlock()
				return fmt.Errorf("failed to observe log line: %w", err)
			}
		}
		c.builderMtx.Unlock()
	}

	// Write the indexes (bloom filters) to the new index object.
	for columnName, bloom := range columnBloomBuilders {
		bloomBytes, err := bloom.MarshalBinary()
		if err != nil {
			return fmt.Errorf("failed to marshal bloom filter: %w", err)
		}
		c.builderMtx.Lock()
		err = c.indexobjBuilder.AppendColumnIndex(objectPath, sectionIdx, columnName, columnIndexes[columnName], bloomBytes)
		c.builderMtx.Unlock()
		if err != nil {
			return fmt.Errorf("failed to append column index: %w", err)
		}
	}

	level.Info(sectionLogger).Log("msg", "finished processing logs section", "rowsProcessed", cnt)
	return nil
}
