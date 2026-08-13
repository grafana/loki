package index

import (
	"bytes"
	"cmp"
	"context"
	"fmt"
	"hash/fnv"
	"slices"
	"strings"
	"time"

	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/streams"
)

// created for and scoped to each logs section
type statsCalculation struct {
	schema       []string                   // schema is the fully-qualified sort schema ("label:<name>")
	labelKeys    []string                   // labelKeys are the bare Prometheus label names derived from schema
	aggregates   map[uint64]*statsAggregate // keyed by hash of composite label values
	streamShards map[int64]uint32
}

type statsAggregate struct {
	shard            uint32
	labels           map[string]string // all sort schema key-value pairs
	minTimestamp     time.Time
	maxTimestamp     time.Time
	rowCount         int
	uncompressedSize int64
}

func (c *statsCalculation) Name() string { return "stats" }

// ProcessBatchNeedsBuilderLock reports whether ProcessBatch mutates the shared
// builder. Stats aggregates into step-local state during ProcessBatch;
// shared-builder writes happen in Flush, so no lock is required.
func (c *statsCalculation) ProcessBatchNeedsBuilderLock() bool { return false }

func (c *statsCalculation) Prepare(_ context.Context, _ *logsCalculationContext, _ *dataobj.Section, _ logs.Stats) error {
	labelKeys, err := schemaLabelNames(c.schema)
	if err != nil {
		return fmt.Errorf("stats calculation: %w", err)
	}
	c.labelKeys = labelKeys
	c.aggregates = make(map[uint64]*statsAggregate)
	c.streamShards = make(map[int64]uint32)
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
		shard, ok := c.streamShards[log.StreamID]
		if !ok {
			shard = uint32(labels.StableHash(streamLbls) % uint64(streams.ShardFactor))
			c.streamShards[log.StreamID] = shard
		}

		// Build the composite key from the shard and all sort schema keys.
		// Uses key=value pairs separated by \x00 to avoid ambiguity.
		buf.Reset()
		buf.WriteByte(byte(shard))
		for _, key := range c.labelKeys {
			buf.WriteByte(0)
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
			labelMap := make(map[string]string, len(c.labelKeys))
			for _, key := range c.labelKeys {
				labelMap[key] = streamLbls.Get(key)
			}
			agg = &statsAggregate{
				shard:        shard,
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
		// The uncompressed_logs_size byte contract is line bytes plus structured
		// metadata value bytes, matching streams.Stream.UncompressedSize recorded
		// during initial indexing (see consumer/logsobj Builder.Append). Counting
		// only the line here would make compaction output disagree with the ToC
		// values written on the initial index flush.
		size := int64(len(log.Line))
		log.Metadata.Range(func(md labels.Label) {
			size += int64(len(md.Value))
		})
		agg.uncompressedSize += size
	}
	return nil
}

func (c *statsCalculation) Flush(_ context.Context, calcCtx *logsCalculationContext) error {
	if len(c.aggregates) == 0 {
		return nil
	}

	// Sort aggregates by shard, then label values in schema key order.
	sorted := make([]*statsAggregate, 0, len(c.aggregates))
	for _, agg := range c.aggregates {
		sorted = append(sorted, agg)
	}
	slices.SortFunc(sorted, func(a, b *statsAggregate) int {
		if n := cmp.Compare(a.shard, b.shard); n != 0 {
			return n
		}
		for _, key := range c.labelKeys {
			if n := cmp.Compare(a.labels[key], b.labels[key]); n != 0 {
				return n
			}
		}
		return 0
	})

	sortSchema := strings.Join(c.schema, ",")
	for _, agg := range sorted {
		err := calcCtx.builder.AppendStatWithLayoutAndShard(
			calcCtx.tenantID,
			calcCtx.objectPath,
			calcCtx.sectionIdx,
			sortSchema,
			calcCtx.physicalLayout.ID(),
			agg.labels,
			agg.minTimestamp,
			agg.maxTimestamp,
			agg.rowCount,
			agg.uncompressedSize,
			agg.shard,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
