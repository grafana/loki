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
	NewBuilder(tenant string) (*logsobj.Builder, error)
}

// MultiObjectBuilder keeps the builders of each tenant and ToC window. A
// tenant with a late log threshold can get a second builder per window, for the
// late lines of that window.
type MultiObjectBuilder struct {
	buildersByToc    map[tocKey]*tocScopedBuilders
	builderFactory   builderFactory
	maxBufferedBytes int
	watermarks       *watermarkCache
	overrides        TenantOverrides
}

// tocKey identifies one ToC: the ToC of a tenant and a time window. The ToC
// watermark belongs to this ToC.
type tocKey struct {
	tenant string
	window time.Time
}

func (k tocKey) String() string {
	return fmt.Sprintf("{tenant=%s, window=%s}", k.tenant, k.window.Format(time.RFC3339))
}

func compareTocKeys(a, b tocKey) int {
	return cmp.Or(strings.Compare(a.tenant, b.tenant), a.window.Compare(b.window))
}

// class tells whether a builder holds the lines that reached Kafka soon after
// their timestamp, or long after it.
type class int

const (
	inTimeClass class = iota
	lateClass
)

func (c class) String() string {
	if c == lateClass {
		return "late"
	}
	return "in_time"
}

// classes lists the classes in the order that the flush handles them.
var classes = []class{inTimeClass, lateClass}

// tocScopedBuilders holds the builders whose indexes go to the same ToC: the
// builder of the in-time lines and the builder of the late lines of one tenant
// and window. A class that has no lines has no builder.
//
// Both builders share the initial watermark: the ToC watermark that the first
// record of the ToC was checked against. The flush expects the ToC to hold it
// when it adds the indexes. Nil means the ToC holds no watermark for the
// partition.
type tocScopedBuilders struct {
	toc              tocKey
	initialWatermark *int64
	builders         map[class]logsBuilder
}

// all returns the builders that exist, in-time first.
func (t *tocScopedBuilders) all() []logsBuilder {
	var result []logsBuilder
	for _, c := range classes {
		if b, ok := t.builders[c]; ok {
			result = append(result, b)
		}
	}
	return result
}

func (t *tocScopedBuilders) estimatedSize() int {
	size := 0
	for _, b := range t.all() {
		size += b.GetEstimatedSize()
	}
	return size
}

// NewMultiObjectBuilder creates a builder group. It keeps the builders of each
// tenant and ToC window. maxBufferedBytes is the combined upper bound across
// all builders. The group drops the records that the ToC watermarks already
// cover.
//
// The group puts a line in a separate builder if its record reached Kafka more
// than the late threshold of the tenant after the line timestamp. It reads the
// threshold for each record, so the threshold can change at runtime.
func NewMultiObjectBuilder(builderFactory builderFactory, maxBufferedBytes int, watermarks *watermarkCache, overrides TenantOverrides) *MultiObjectBuilder {
	return &MultiObjectBuilder{
		watermarks:       watermarks,
		buildersByToc:    make(map[tocKey]*tocScopedBuilders),
		builderFactory:   builderFactory,
		maxBufferedBytes: maxBufferedBytes,
		overrides:        overrides,
	}
}

var _ multiBuilder = (*MultiObjectBuilder)(nil)

// AppendResult counts what Append did with the ToCs of a record.
type AppendResult struct {
	// Appended is the number of builders that were appended to.
	Appended int
	// Dropped is the number of dropped substreams due to duplication.
	Dropped int
}

func (m *MultiObjectBuilder) Append(ctx context.Context, tenant string, stream logproto.Stream, recTime time.Time, offset int64) (AppendResult, error) {
	var res AppendResult

	// A record can hold entries for several time windows. Split it into one stream for each window. Each window
	// has an in-time stream and a late stream, if it has lines of that class.
	//
	// A line is late if recTime is more than the late threshold after the line timestamp. Every other line is
	// in-time. A record without a valid timestamp has only in-time lines. Use recTime, not the wall clock. The
	// split then does not depend on how far the builder lags behind Kafka.
	threshold := m.overrides.DataObjLateLogsThreshold(tenant)
	if !recTime.After(time.Unix(0, 0)) {
		threshold = 0
	}
	streamsByWindow := make(map[time.Time]map[class]logproto.Stream, 1)
	for _, e := range stream.Entries {
		w := e.Timestamp.UTC().Truncate(metastore.MetastoreWindowSize)
		streams, ok := streamsByWindow[w]
		if !ok {
			streams = make(map[class]logproto.Stream, 1)
			streamsByWindow[w] = streams
		}
		streamClass := inTimeClass
		if threshold > 0 && recTime.Sub(e.Timestamp) > threshold {
			streamClass = lateClass
		}
		s, ok := streams[streamClass]
		if !ok {
			s = logproto.Stream{Labels: stream.Labels}
		}
		s.Entries = append(s.Entries, e)
		streams[streamClass] = s
	}

	for window, streams := range streamsByWindow {
		toc := tocKey{tenant: tenant, window: window}
		sb, exists := m.buildersByToc[toc]
		if !exists {
			initialWatermark, err := m.watermarks.get(ctx, toc)
			if err != nil {
				return res, fmt.Errorf("%w: %w", errUnrecoverable, err)
			}
			sb = &tocScopedBuilders{toc: toc, initialWatermark: initialWatermark, builders: make(map[class]logsBuilder, 1)}
		}
		// The ToC watermark covers the whole window, so a record is dropped or kept
		// as a whole, whether its lines are late or in-time.
		if sb.initialWatermark != nil && offset <= *sb.initialWatermark {
			res.Dropped++
			continue
		}

		for streamClass, stream := range streams {
			b, ok := sb.builders[streamClass]
			if !ok {
				var err error
				if b, err = m.builderFactory.NewBuilder(tenant); err != nil {
					return res, fmt.Errorf("error creating logsobj builder for %s, class %s, offset %d: %w", toc, streamClass, offset, err)
				}
			}
			if err := b.Append(stream, recTime); err != nil {
				return res, fmt.Errorf("append for %s, class %s, offset %d: %w", toc, streamClass, offset, err)
			}
			// Only store a new builder if append succeeded to avoid empty builders added to the group
			// on Append errors.
			sb.builders[streamClass] = b
			m.buildersByToc[toc] = sb
			res.Appended++
		}
	}

	return res, nil
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
	for _, t := range m.buildersByToc {
		size += t.estimatedSize()
	}
	return size
}

// Reset drops the builders after their flush was handed off.
//
// The flush raises the ToC watermark of each ToC to flushOffset (inclusive).
// Reset stores flushOffset in the watermark cache, so the next record of a ToC
// does not read the ToC from the bucket again. This assumes that only one
// process handles the partition.
//
// If a flush fails, the processor stops and its restart discards the cache. If
// another process changes a watermark, the next flush of that ToC fails,
// because the ToC no longer holds the expected watermark. The builder then
// restarts and reads the watermarks again.
func (m *MultiObjectBuilder) Reset(flushOffset int64) {
	for toc := range m.buildersByToc {
		m.watermarks.set(toc, flushOffset)
	}
	clear(m.buildersByToc)
}

// getBuilders returns the builders of all ToCs, sorted by tenant and then by
// ascending window start time.
//
// The order does not depend on Go's map iteration, so the same builders always
// come back in the same order. This makes tests and operational reasoning
// simpler.
func (m *MultiObjectBuilder) getBuilders() []*tocScopedBuilders {
	tocs := slices.SortedFunc(maps.Keys(m.buildersByToc), compareTocKeys)

	result := make([]*tocScopedBuilders, 0, len(tocs))
	for _, toc := range tocs {
		result = append(result, m.buildersByToc[toc])
	}
	return result
}
