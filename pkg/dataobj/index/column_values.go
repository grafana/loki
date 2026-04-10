package index

import (
	"context"
	"fmt"
	"time"

	"github.com/bits-and-blooms/bloom/v3"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/memory"
)

// created for and scoped to each logs section
type columnValuesCalculation struct {
	columnBloomBuilders map[string]*bloom.BloomFilter
	columnIndexes       map[string]int64

	// Per-column tracking for AppendBloomPosting.
	columnStreamBitmaps map[string]*memory.Bitmap
	columnMinTimestamps map[string]time.Time
	columnMaxTimestamps map[string]time.Time
	columnSizes         map[string]int64
	maxStreamID         int64
}

func (c *columnValuesCalculation) Name() string { return "column_values" }

func (c *columnValuesCalculation) Prepare(_ context.Context, _ *dataobj.Section, stats logs.Stats) error {
	c.columnBloomBuilders = make(map[string]*bloom.BloomFilter)
	c.columnIndexes = make(map[string]int64)
	c.columnStreamBitmaps = make(map[string]*memory.Bitmap)
	c.columnMinTimestamps = make(map[string]time.Time)
	c.columnMaxTimestamps = make(map[string]time.Time)
	c.columnSizes = make(map[string]int64)
	c.maxStreamID = 0

	for _, column := range stats.Columns {
		logsType, _ := logs.ParseColumnType(column.Type)
		if logsType != logs.ColumnTypeMetadata {
			continue
		}
		c.columnBloomBuilders[column.Name] = bloom.NewWithEstimates(uint(column.Cardinality), 1.0/128.0)
		c.columnIndexes[column.Name] = column.ColumnIndex
		bm := memory.NewBitmap(nil, 0)
		c.columnStreamBitmaps[column.Name] = &bm
	}
	return nil
}

func (c *columnValuesCalculation) ProcessBatch(_ context.Context, _ *logsCalculationContext, batch []logs.Record) error {
	for _, log := range batch {
		if log.StreamID > c.maxStreamID {
			c.maxStreamID = log.StreamID
		}

		log.Metadata.Range(func(md labels.Label) {
			bf, ok := c.columnBloomBuilders[md.Name]
			if !ok {
				return
			}

			bf.Add([]byte(md.Value))

			// columnStreamBitmaps was populated with the same metadata keys as
			// columnBloomBuilders in Prepare, so the bitmap will exist
			bm := c.columnStreamBitmaps[md.Name]
			if int(log.StreamID) >= bm.Len() {
				bm.Resize(int(log.StreamID) + 1)
			}
			bm.Set(int(log.StreamID), true)

			if ts, ok := c.columnMinTimestamps[md.Name]; !ok || log.Timestamp.Before(ts) {
				c.columnMinTimestamps[md.Name] = log.Timestamp
			}
			if ts, ok := c.columnMaxTimestamps[md.Name]; !ok || log.Timestamp.After(ts) {
				c.columnMaxTimestamps[md.Name] = log.Timestamp
			}
			c.columnSizes[md.Name] += int64(len(log.Line))
		})
	}
	return nil
}

func (c *columnValuesCalculation) Flush(_ context.Context, context *logsCalculationContext) error {
	for columnName, bloomFilter := range c.columnBloomBuilders {
		bloomBytes, err := bloomFilter.MarshalBinary()
		if err != nil {
			return fmt.Errorf("failed to marshal bloom filter: %w", err)
		}
		err = context.builder.AppendColumnIndex(context.tenantID, context.objectPath, context.sectionIdx, columnName, c.columnIndexes[columnName], bloomBytes)
		if err != nil {
			return fmt.Errorf("failed to append column index: %w", err)
		}

		// Append bloom posting with stream ID bitmap.
		if bm, ok := c.columnStreamBitmaps[columnName]; ok {
			targetSize := int(c.maxStreamID) + 1
			if bm.Len() < targetSize {
				bm.Resize(targetSize)
			}
			bitmapData, _ := bm.Bytes()

			minTs := c.columnMinTimestamps[columnName]
			maxTs := c.columnMaxTimestamps[columnName]
			size := c.columnSizes[columnName]

			err = context.builder.AppendBloomPosting(
				context.tenantID,
				context.objectPath,
				context.sectionIdx,
				columnName,
				bloomBytes,
				bitmapData,
				size,
				minTs,
				maxTs,
			)
			if err != nil {
				return fmt.Errorf("failed to append bloom posting: %w", err)
			}
		}
	}
	return nil
}
