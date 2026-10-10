package index

import (
	"context"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// created for and scoped to each logs section
type columnValuesCalculation struct {
	columns map[string]struct{} // names of the metadata columns
}

func (c *columnValuesCalculation) Name() string { return "column_values" }

// ProcessBatchNeedsBuilderLock reports whether ProcessBatch mutates the shared
// builder. Column values calls builder.ObserveBloomPosting per matching metadata
// label, which writes into per-column postings state on the shared builder, so
// it must run under the builder lock.
func (c *columnValuesCalculation) ProcessBatchNeedsBuilderLock() bool { return true }

func (c *columnValuesCalculation) Prepare(_ context.Context, calcCtx *logsCalculationContext, _ *dataobj.Section, stats logs.Stats) error {
	c.columns = make(map[string]struct{})

	for _, column := range stats.Columns {
		logsType, _ := logs.ParseColumnType(column.Type)
		if logsType != logs.ColumnTypeMetadata {
			continue
		}
		c.columns[column.Name] = struct{}{}
		calcCtx.builder.PrepareBloomColumn(
			calcCtx.objectPath, calcCtx.sectionIdx,
			column.Name, uint(column.Cardinality), int64(streams.ShardFactor),
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
			if _, ok := c.columns[md.Name]; !ok {
				return
			}
			batchErr = calcCtx.builder.ObserveBloomPosting(postings.BloomObservation{
				ObjectPath:       calcCtx.objectPath,
				ShardBuckets:     int64(streams.ShardFactor),
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

// Flush does nothing, because ProcessBatch writes the bloom postings directly
// to the builder.
func (c *columnValuesCalculation) Flush(_ context.Context, _ *logsCalculationContext) error {
	return nil
}
