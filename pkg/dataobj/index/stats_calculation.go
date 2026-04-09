package index

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"hash"
	"hash/fnv"
	"sort"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
)

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
	// Reuse a single hasher across all records. hash.Reset() retains the
	// internal buffer, so this avoids per-record allocations on the hot path.
	// The hash is only used as a map key; actual label values are stored in the
	// aggregate's labels map (created only on first encounter).
	var h = fnv.New64a()
	for _, log := range batch {
		streamLbls := calcCtx.streamLabels[log.StreamID]

		// Hash the composite key from all sort schema keys.
		// Uses key=value pairs separated by \x00 to avoid ambiguity.
		h.Reset()
		for i, key := range c.sortSchemaKeys {
			value := streamLbls.Get(key)
			err := writeHashKey(h, i, key, value)
			if err != nil {
				return err
			}
		}

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

func writeHashKey(h hash.Hash64, i int, key, value string) error {
	var err error
	if i > 0 {
		// write delimeter value in between key/value pairs
		_, err = h.Write([]byte{0})
		if err != nil {
			return err
		}
	}
	_, err = h.Write([]byte(key))
	if err != nil {
		return err
	}

	_, err = h.Write([]byte{'='})
	if err != nil {
		return err
	}

	_, err = h.Write([]byte(value))
	if err != nil {
		return err
	}

	return nil
}

func (c *statsCalculation) Flush(_ context.Context, calcCtx *logsCalculationContext) error {
	if len(c.aggregates) == 0 {
		return nil
	}

	// Compute run-ID from object path using SHA-224.
	// We take the first 8 bytes as an int64.
	sum := sha256.Sum224([]byte(calcCtx.objectPath))
	runID := int64(binary.BigEndian.Uint64(sum[:8]))

	// Sort aggregates by label values in schema key order
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
			runID,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
