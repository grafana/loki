package index

import (
	"context"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

type statsCalculation struct {
	sortSchemaKeys []string                   // label keys to aggregate by
	aggregates     map[string]*statsAggregate // keyed by label value
}

type statsAggregate struct {
	labelValue       string
	minTimestamp     time.Time
	maxTimestamp     time.Time
	rowCount         int
	uncompressedSize int64
}

func (c *statsCalculation) Prepare(_ context.Context, _ *dataobj.Section, _ logs.Stats) error {
	c.aggregates = make(map[string]*statsAggregate)
	return nil
}

func (c *statsCalculation) ProcessBatch(_ context.Context, calcCtx *logsCalculationContext, batch []logs.Record) error {
	for _, log := range batch {
		streamLbls := calcCtx.streamLabels[log.StreamID]
		// For now, sort schema has a single key. When multi-key schemas are
		// supported, this loop will aggregate per composite key.
		labelValue := streamLbls.Get(c.sortSchemaKeys[0])

		agg, ok := c.aggregates[labelValue]
		if !ok {
			agg = &statsAggregate{
				labelValue:   labelValue,
				minTimestamp: log.Timestamp,
				maxTimestamp: log.Timestamp,
			}
			c.aggregates[labelValue] = agg
		}

		if log.Timestamp.Before(agg.minTimestamp) {
			agg.minTimestamp = log.Timestamp
		}
		if log.Timestamp.After(agg.maxTimestamp) {
			agg.maxTimestamp = log.Timestamp
		}
		agg.rowCount++
		agg.uncompressedSize += int64(len(log.Line))
	}
	return nil
}

func (c *statsCalculation) Flush(_ context.Context, calcCtx *logsCalculationContext) error {
	if len(c.aggregates) == 0 {
		return nil
	}

	// Compute run-ID from object path.
	h := fnv.New64a()
	h.Write([]byte(calcCtx.objectPath))
	runID := int64(h.Sum64())

	// Sort aggregates by label value for deterministic output.
	sorted := make([]*statsAggregate, 0, len(c.aggregates))
	for _, agg := range c.aggregates {
		sorted = append(sorted, agg)
	}
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].labelValue < sorted[j].labelValue
	})

	for _, agg := range sorted {
		err := calcCtx.builder.AppendStat(
			calcCtx.tenantID,
			calcCtx.objectPath,
			calcCtx.sectionIdx,
			strings.Join(c.sortSchemaKeys, ","),
			agg.labelValue,
			agg.minTimestamp,
			agg.maxTimestamp,
			agg.rowCount,
			agg.uncompressedSize,
			runID,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
