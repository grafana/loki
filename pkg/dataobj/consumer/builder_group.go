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

// TOCAlignedBuilderGroup manages a set of [logsobj.Builder] instances, one per
// 12-hour UTC window (see [metastore.TOCWindowSize]). Each builder in the
// group only contains entries whose timestamps fall into that window, which
// keeps per-object time ranges aligned with the metastore TOC and bounds the
// number of windows a single object can cover.
//
// The group's [IsFull] signal is driven by the *sum* of estimated sizes across
// all window-specific builders, so the group preserves the "a partition will
// not buffer more than TargetObjectSize" invariant that existed before the
// per-window split.
type TOCAlignedBuilderGroup struct {
	builders         map[time.Time]builder
	builderFactory   builderFactory
	targetObjectSize int
}

// NewTOCAlignedBuilderGroup creates a new TOC aligned builder group.
// targetObjectSize is the combined upper bound across all window builders; it
// should match the TargetObjectSize used by the builder factory.
func NewTOCAlignedBuilderGroup(builderFactory builderFactory, targetObjectSize int) *TOCAlignedBuilderGroup {
	return &TOCAlignedBuilderGroup{
		builders:         make(map[time.Time]builder),
		builderFactory:   builderFactory,
		targetObjectSize: targetObjectSize,
	}
}

var _ builderGroup = (*TOCAlignedBuilderGroup)(nil)

func (g *TOCAlignedBuilderGroup) Append(tenant string, stream logproto.Stream, recTime time.Time) error {
	// [stream] might contain entries for multiple time windows => we need to split them to multiple streams, where
	// each one only contains the entries for a single time window
	streamsByTimeWindows := make(map[time.Time]logproto.Stream, 1)
	for _, e := range stream.Entries {
		w := e.Timestamp.UTC().Truncate(metastore.TOCWindowSize)
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
		b, exists := g.builders[w]
		if !exists {
			var err error
			b, err = g.builderFactory.NewBuilder()
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
			g.builders[w] = b
		}
	}

	return nil
}

// IsFull reports whether the combined buffered size across all window
// builders has exceeded the configured target object size.
//
// We intentionally compare the *sum* (rather than each individual builder's
// IsFull) so that a partition that receives records spanning many windows is
// still bounded to roughly TargetObjectSize bytes of buffered memory, rather
// than K*TargetObjectSize.
func (g *TOCAlignedBuilderGroup) IsFull() bool {
	return g.GetEstimatedSize() > g.targetObjectSize
}

func (g *TOCAlignedBuilderGroup) GetEstimatedSize() int {
	size := 0
	for _, b := range g.builders {
		size += b.GetEstimatedSize()
	}
	return size
}

func (g *TOCAlignedBuilderGroup) Reset() {
	clear(g.builders)
}

// GetBuilders returns all per-window builders, sorted in ascending order of
// window start time.
//
// A deterministic order keeps flush-time side effects (metastore event
// writes, offset commits, and any future per-window metrics) predictable
// regardless of Go's map iteration randomization, which simplifies both
// tests and operational reasoning.
func (g *TOCAlignedBuilderGroup) GetBuilders() []builder {
	windows := make([]time.Time, 0, len(g.builders))
	for w := range g.builders {
		windows = append(windows, w)
	}
	sort.Slice(windows, func(i, j int) bool { return windows[i].Before(windows[j]) })

	result := make([]builder, 0, len(g.builders))
	for _, w := range windows {
		result = append(result, g.builders[w])
	}
	return result
}
