package index

import (
	"context"
	"fmt"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// created for and scoped to each logs section
type columnValuesCalculation struct {
	columnIndexes map[string]int64
}

func (c *columnValuesCalculation) Name() string { return "column_values" }

// ProcessBatchNeedsBuilderLock reports whether ProcessBatch mutates the shared
// builder. Column values calls builder.ObserveBloomPosting per matching metadata
// label, which writes into per-column postings state on the shared builder, so
// it must run under the builder lock.
func (c *columnValuesCalculation) ProcessBatchNeedsBuilderLock() bool { return true }

func (c *columnValuesCalculation) Prepare(_ context.Context, calcCtx *logsCalculationContext, _ *dataobj.Section, stats logs.Stats) error {
	c.columnIndexes = make(map[string]int64)

	for _, column := range stats.Columns {
		logsType, _ := logs.ParseColumnType(column.Type)
		if logsType != logs.ColumnTypeMetadata {
			continue
		}
		c.columnIndexes[column.Name] = column.ColumnIndex
		calcCtx.builder.PrepareBloomColumn(
			calcCtx.tenantID, calcCtx.objectPath, calcCtx.sectionIdx,
			column.Name, uint(column.Cardinality),
		)
	}
	return nil
}

func (c *columnValuesCalculation) ProcessBatch(_ context.Context, calcCtx *logsCalculationContext, batch []logs.Record) error {
	var batchErr error
	for _, log := range batch {
		if batchErr != nil {
			break
		}
		log.Metadata.Range(func(md labels.Label) {
			if batchErr != nil {
				return
			}
			if _, ok := c.columnIndexes[md.Name]; !ok {
				return
			}
			batchErr = calcCtx.builder.ObserveBloomPosting(calcCtx.tenantID, postings.BloomObservation{
				ObjectPath:       calcCtx.objectPath,
				SectionIndex:     calcCtx.sectionIdx,
				ColumnName:       md.Name,
				Value:            md.Value,
				StreamID:         log.StreamID,
				Timestamp:        log.Timestamp,
				UncompressedSize: int64(len(log.Line)),
			})
		})
	}
	return batchErr
}

func (c *columnValuesCalculation) Flush(_ context.Context, calcCtx *logsCalculationContext) error {
	for columnName := range c.columnIndexes {
		bloomBytes, err := calcCtx.builder.BloomBytes(
			calcCtx.tenantID, calcCtx.objectPath, calcCtx.sectionIdx, columnName,
		)
		if err != nil {
			return fmt.Errorf("failed to get bloom bytes for %s: %w", columnName, err)
		}
		err = calcCtx.builder.AppendColumnIndex(
			calcCtx.tenantID, calcCtx.objectPath, calcCtx.sectionIdx,
			columnName, c.columnIndexes[columnName], bloomBytes,
		)
		if err != nil {
			return fmt.Errorf("failed to append column index: %w", err)
		}
	}
	return nil
}
