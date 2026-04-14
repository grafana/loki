package index

import (
	"bytes"
	"cmp"
	"context"
	"hash/fnv"
	"slices"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

// created for and scoped to each logs section
type statsCalculation struct {
	sortSchemaKeys []string                   // label keys to aggregate by
	aggregates     map[uint64]*statsAggregate // keyed by hash of composite label values
}

type statsAggregate struct {
	labels           map[string]string // all sort schema key-value pairs
	minTimestamp     time.Time
	maxTimestamp     time.Time
	rowCount         int
	uncompressedSize int64
}

func (c *statsCalculation) Name() string { return "stats" }

func (c *statsCalculation) Prepare(_ context.Context, _ *dataobj.Section, _ logs.Stats) error {
	c.aggregates = make(map[uint64]*statsAggregate)
	return nil
}

func (c *statsCalculation) ProcessBatch(_ context.Context, calcCtx *logsCalculationContext, batch []logs.Record) error {
	// Reuse a single hasher and buffer across all records to avoid
	// per-record allocations on the hot path.
	var (
		h   = fnv.New64a()
		buf bytes.Buffer
	)
	for _, log := range batch {
		streamLbls := calcCtx.streamLabels[log.StreamID]

		// Build the composite key from all sort schema keys.
		// Uses key=value pairs separated by \x00 to avoid ambiguity.
		buf.Reset()
		for i, key := range c.sortSchemaKeys {
			if i > 0 {
				buf.WriteByte(0)
			}

			buf.WriteString(key)
			buf.WriteByte('=')
			buf.WriteString(streamLbls.Get(key))
		}
		h.Reset()
		h.Write(buf.Bytes())

		aggKey := h.Sum64()
		agg, ok := c.aggregates[aggKey]
		if !ok {
			// Only allocate the labels map when creating a new aggregate.
			labelMap := make(map[string]string, len(c.sortSchemaKeys))
			for _, key := range c.sortSchemaKeys {
				labelMap[key] = streamLbls.Get(key)
			}
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

	// Sort aggregates by label values in schema key order
	sorted := make([]*statsAggregate, 0, len(c.aggregates))
	for _, agg := range c.aggregates {
		sorted = append(sorted, agg)
	}
	slices.SortFunc(sorted, func(a, b *statsAggregate) int {
		for _, key := range c.sortSchemaKeys {
			if n := cmp.Compare(a.labels[key], b.labels[key]); n != 0 {
				return n
			}
		}
		return 0
	})

	sortSchema := strings.Join(c.sortSchemaKeys, ",")
	for _, agg := range sorted {
		err := calcCtx.builder.AppendStat(
			calcCtx.tenantID,
			calcCtx.objectPath,
			calcCtx.sectionIdx,
			sortSchema,
			agg.labels,
			agg.minTimestamp,
			agg.maxTimestamp,
			agg.rowCount,
			agg.uncompressedSize,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
