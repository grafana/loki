package consumer

import (
	"fmt"
	"sort"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/consumer/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type builderFactory interface {
	NewBuilder() (*logsobj.Builder, error)
}

// TOCAlignedMultiBuilder manages a set of [logsobj.Builder] instances, one per
// 12-hour UTC window (see [metastore.MetastoreWindowSize]). Each builder in the
// group only contains entries whose timestamps fall into that window, which
// keeps per-object time ranges aligned with the metastore TOC and bounds the
// number of windows a single object can cover.
type TOCAlignedMultiBuilder struct {
	builders         map[time.Time]builder
	builderFactory   builderFactory
	maxBufferedBytes int
}

// NewTOCAlignedMultiBuilder creates a new TOC aligned builder group.
// maxBufferedBytes is the combined upper bound across all window builders;
func NewTOCAlignedMultiBuilder(builderFactory builderFactory, maxBufferedBytes int) *TOCAlignedMultiBuilder {
	return &TOCAlignedMultiBuilder{
		builders:         make(map[time.Time]builder),
		builderFactory:   builderFactory,
		maxBufferedBytes: maxBufferedBytes,
	}
}

var _ multiBuilder = (*TOCAlignedMultiBuilder)(nil)

func (m *TOCAlignedMultiBuilder) Append(tenant string, stream logproto.Stream, recTime time.Time) error {
	// [stream] might contain entries for multiple time windows => we need to split them to multiple streams, where
	// each one only contains the entries for a single time window
	streamsByTimeWindows := make(map[time.Time]logproto.Stream, 1)
	for _, e := range stream.Entries {
		w := e.Timestamp.UTC().Truncate(metastore.MetastoreWindowSize)
		if _, ok := streamsByTimeWindows[w]; !ok {
			streamsByTimeWindows[w] = logproto.Stream{
				Labels: stream.Labels,
			}
		}
		windowedStream := streamsByTimeWindows[w]
		windowedStream.Entries = append(windowedStream.Entries, e)
		streamsByTimeWindows[w] = windowedStream
	}

	for w, stream := range streamsByTimeWindows {
		b, exists := m.builders[w]
		if !exists {
			var err error
			b, err = m.builderFactory.NewBuilder()
			if err != nil {
				return fmt.Errorf("error creating logsobj builder for window %s: %w", w.Format(time.RFC3339), err)
			}
		}
		if err := b.Append(tenant, stream, recTime); err != nil {
			return fmt.Errorf("append for window %s: %w", w.Format(time.RFC3339), err)
		}

		if !exists {
			// Only store a new builder into the map if append succeeded to avoid empty builders added to the map
			// on Append errors.
			m.builders[w] = b
		}
	}

	return nil
}

// IsFull reports whether the combined buffered size across all window
// builders has exceeded the configured target object size.
//
// We intentionally compare the *sum* (rather than each individual builder's
// IsFull) so that a partition that receives records spanning many windows is
// still bounded to roughly MaxBufferedBytes of buffered memory, rather
// than K*MaxBufferedBytes.
func (m *TOCAlignedMultiBuilder) IsFull() bool {
	return m.GetEstimatedSize() > m.maxBufferedBytes
}

func (m *TOCAlignedMultiBuilder) GetEstimatedSize() int {
	size := 0
	for _, b := range m.builders {
		size += b.GetEstimatedSize()
	}
	return size
}

func (m *TOCAlignedMultiBuilder) Reset() {
	clear(m.builders)
}

// GetBuilders returns all per-window builders, sorted in ascending order of
// window start time.
//
// A deterministic order keeps flush-time side effects (metastore event
// writes, offset commits, and any future per-window metrics) predictable
// regardless of Go's map iteration randomization, which simplifies both
// tests and operational reasoning.
func (m *TOCAlignedMultiBuilder) GetBuilders() []builder {
	windows := make([]time.Time, 0, len(m.builders))
	for w := range m.builders {
		windows = append(windows, w)
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].Before(windows[j]) })

	result := make([]builder, 0, len(m.builders))
	for _, w := range windows {
		result = append(result, m.builders[w])
	}
	return result
}
