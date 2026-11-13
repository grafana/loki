package builder

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/grafana/loki/v3/pkg/dataobj/logsobj"
	"github.com/grafana/loki/v3/pkg/dataobj/metastore"
	"github.com/grafana/loki/v3/pkg/logproto"
)

type builderFactory interface {
	NewBuilder() (*logsobj.Builder, error)
}

// MultiObjectBuilder keeps one [logsobj.Builder] per tenant and ToC window.
type MultiObjectBuilder struct {
	builders         map[builderScope]WatermarkedBuilder
	builderFactory   builderFactory
	maxBufferedBytes int
	watermarks       *watermarkCache
}

// builderScope identifies the entries a single builder in a
// [MultiObjectBuilder] accepts.
type builderScope struct {
	tenant string
	window time.Time
}

func (s builderScope) String() string {
	return fmt.Sprintf("{tenant=%s, window=%s}", s.tenant, s.window.Format(time.RFC3339))
}

func compareScopes(a, b builderScope) int {
	return cmp.Or(strings.Compare(a.tenant, b.tenant), a.window.Compare(b.window))
}

// NewMultiObjectBuilder creates a new builder group scoped by tenant and ToC
// window. maxBufferedBytes is the combined upper bound across all builders.
// The group drops the records that the ToC watermarks already cover.
func NewMultiObjectBuilder(builderFactory builderFactory, maxBufferedBytes int, watermarks *watermarkCache) *MultiObjectBuilder {
	return &MultiObjectBuilder{
		watermarks:       watermarks,
		builders:         make(map[builderScope]WatermarkedBuilder),
		builderFactory:   builderFactory,
		maxBufferedBytes: maxBufferedBytes,
	}
}

var _ multiBuilder = (*MultiObjectBuilder)(nil)

// AppendResult counts what Append did with the scopes of a record.
type AppendResult struct {
	// Appended is the number of builders that were appended to.
	Appended int
	// Dropped is the number of dropped substreams due to duplication.
	Dropped int
}

func (m *MultiObjectBuilder) Append(ctx context.Context, tenant string, stream logproto.Stream, recTime time.Time, offset int64) (AppendResult, error) {
	var res AppendResult

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
		scope := builderScope{tenant: tenant, window: w}
		initialWatermark, err := m.initialWatermark(ctx, scope)
		if err != nil {
			return res, fmt.Errorf("%w: %w", errUnrecoverable, err)
		}
		if initialWatermark != nil && offset <= *initialWatermark {
			res.Dropped++
			continue
		}
		b, exists := m.builders[scope]
		if !exists {
			nb, err := m.builderFactory.NewBuilder()
			if err != nil {
				return res, fmt.Errorf("error creating logsobj builder for scope %s: %w", scope, err)
			}
			b = WatermarkedBuilder{logsBuilder: nb, initialWatermark: initialWatermark}
		}
		if err := b.Append(tenant, stream, recTime); err != nil {
			return res, fmt.Errorf("append for scope %s: %w", scope, err)
		}

		if !exists {
			// Only store a new builder into the map if append succeeded to avoid empty builders added to the map
			// on Append errors.
			m.builders[scope] = b
		}
		res.Appended++
	}

	return res, nil
}

// initialWatermark returns the ToC watermark that a record of the scope is
// checked against: the initial watermark of the scope's builder if it exists, or
// else the cached watermark.
func (m *MultiObjectBuilder) initialWatermark(ctx context.Context, scope builderScope) (*int64, error) {
	if b, ok := m.builders[scope]; ok {
		return b.initialWatermark, nil
	}
	return m.watermarks.get(ctx, scope)
}

// IsFull reports whether the combined buffered size across all builders has
// exceeded the configured target object size.
//
// We intentionally compare the *sum* (rather than each individual builder's
// IsFull) so that a partition that receives records spanning many tenants and
// windows is still bounded to roughly MaxBufferedBytes of buffered memory,
// rather than K*MaxBufferedBytes.
func (m *MultiObjectBuilder) IsFull() bool {
	return m.GetEstimatedSize() > m.maxBufferedBytes
}

func (m *MultiObjectBuilder) GetEstimatedSize() int {
	size := 0
	for _, b := range m.builders {
		size += b.GetEstimatedSize()
	}
	return size
}

// Reset drops the builders after their flush was handed off.
//
// The flush raises the ToC watermark of each scope to flushOffset (inclusive).
// Reset stores flushOffset in the watermark cache, so the next record of a scope
// does not read the ToC from the bucket again. This assumes that only one
// process handles the partition.
//
// If a flush fails, the processor stops and its restart discards the cache. If
// another process changes a watermark, the next flush of that scope fails,
// because the ToC no longer holds the expected watermark. The builder then
// restarts and reads the watermarks again.
func (m *MultiObjectBuilder) Reset(flushOffset int64) {
	for scope := range m.builders {
		m.watermarks.set(scope, flushOffset)
	}
	clear(m.builders)
}

// WatermarkedBuilder decorates a builder with the ToC watermark that its first
// record was checked against. The flush expects the ToC to hold this watermark
// when it adds the builder's index. Nil means the ToC holds no watermark for the
// partition. A WatermarkedBuilder is immutable.
type WatermarkedBuilder struct {
	logsBuilder
	initialWatermark *int64
}

// InitialWatermark returns the ToC watermark that the first record of the
// builder was checked against.
func (b WatermarkedBuilder) InitialWatermark() *int64 {
	return b.initialWatermark
}

// getBuilders returns all builders, sorted by tenant and then by ascending
// window start time.
//
// The order does not depend on Go's map iteration, so the same builders always
// come back in the same order. This makes tests and operational reasoning
// simpler.
func (m *MultiObjectBuilder) getBuilders() []builder {
	scopes := slices.SortedFunc(maps.Keys(m.builders), compareScopes)

	result := make([]builder, 0, len(scopes))
	for _, s := range scopes {
		result = append(result, m.builders[s])
	}
	return result
}
