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
	aggregates     map[string]*statsAggregate // keyed by composite label value
}

type statsAggregate struct {
	labels           map[string]string // all sort schema key-value pairs
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

		// Build composite key and labels map from all sort schema keys.
		// The composite key uses key=value pairs separated by \x00 to avoid
		// ambiguity from values containing commas or other delimiters.
		labelMap := make(map[string]string, len(c.sortSchemaKeys))
		var compositeKey strings.Builder
		for i, key := range c.sortSchemaKeys {
			val := streamLbls.Get(key)
			labelMap[key] = val
			if i > 0 {
				compositeKey.WriteByte(0)
			}
			compositeKey.WriteString(key)
			compositeKey.WriteByte('=')
			compositeKey.WriteString(val)
		}

		aggKey := compositeKey.String()
		agg, ok := c.aggregates[aggKey]
		if !ok {
			agg = &statsAggregate{
				labels:       labelMap,
				minTimestamp: log.Timestamp,
				maxTimestamp: log.Timestamp,
			}
			c.aggregates[aggKey] = agg
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

	// Sort aggregates by label values in schema key order for deterministic output.
	sorted := make([]*statsAggregate, 0, len(c.aggregates))
	for _, agg := range c.aggregates {
		sorted = append(sorted, agg)
	}
	sort.Slice(sorted, func(i, j int) bool {
		for _, key := range c.sortSchemaKeys {
			vi, vj := sorted[i].labels[key], sorted[j].labels[key]
			if vi != vj {
				return vi < vj
			}
		}
		return false
	})

	for _, agg := range sorted {
		err := calcCtx.builder.AppendStat(
			calcCtx.tenantID,
			calcCtx.objectPath,
			calcCtx.sectionIdx,
			strings.Join(c.sortSchemaKeys, ","),
			agg.labels,
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
