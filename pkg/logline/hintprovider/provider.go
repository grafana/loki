package hintprovider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logline/store"
	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// ErrUnsupported is returned when the query shape cannot be handled by the
// logline index hint provider. ErrUnconstrained is returned when the query
// cannot be narrowed by logline.
var (
	ErrUnsupported   = errors.New("query not supported by logline index")
	ErrUnconstrained = errors.New("logline index cannot narrow this query")
)

const maxLoggedHintRanges = 10

// hintRangeTimeFormat is RFC3339 with millisecond precision for compact log output.
const hintRangeTimeFormat = "2006-01-02T15:04:05.000Z07:00"

// HintSourcePreMinDate marks synthetic diagnostic provenance for ranges
// representing query windows before store min_date. This must not be used for
// required behavior because Source is intentionally omitted from cache payloads.
const HintSourcePreMinDate = "pre_min_date"

// QueryHintProvider inspects a query and returns narrowed scan hints.
type QueryHintProvider interface {
	ProvideHints(ctx context.Context, tenant string, expr syntax.Expr, from, through model.Time) (*Hints, *QueryStats, error)
}

type HintPlan struct {
	Filters     []string
	Ranges      []HintTimeRange // pre-min-date passthrough, if any
	Indexes     []store.Meta
	Stats       *QueryStats
	NgramLength int32
	MaxParallel int32
}

// HintTimeRange is a half-open time window [Start, End) that may contain
// matching logs. Start is inclusive, End is exclusive. This matches the
// document bounds written by index builders (MinTimeUnix inclusive,
// MaxTimeUnix exclusive).
type HintTimeRange struct {
	Start time.Time
	End   time.Time
	// Source is optional provider-specific provenance for diagnostics.
	Source string
}

// IsPassthrough returns true for synthetic hint ranges that represent time
// windows where the logline index has no coverage (e.g. before store min
// date). These ranges use a zero-value Start as a sentinel. An interval
// covered only by this sentinel is passed through unmodified. Mixed with
// indexed ranges, the overlap is attached as a concrete hint.
func (h HintTimeRange) IsPassthrough() bool {
	return h.Start.IsZero()
}

// passthroughForInclusiveThrough covers an inclusive ProvideHints/LoglineIndex
// through bound as a half-open hint. Zero Start is the passthrough sentinel;
// End is the next millisecond so a log at through stays inside [Start, End).
func passthroughForInclusiveThrough(through time.Time) HintTimeRange {
	return HintTimeRange{End: through.UTC().Add(time.Millisecond)}
}

// Hints contains narrowed ranges derived from index lookups.
type Hints struct {
	TimeRanges []HintTimeRange
}

func ToProtoRanges(in []HintTimeRange) []logproto.HintTimeRange {
	out := make([]logproto.HintTimeRange, len(in))
	for i, r := range in {
		out[i] = logproto.HintTimeRange{Start: r.Start, End: r.End}
	}
	return out
}

func FromProtoRanges(in []logproto.HintTimeRange) []HintTimeRange {
	out := make([]HintTimeRange, len(in))
	for i, r := range in {
		out[i] = HintTimeRange{Start: r.Start, End: r.End}
	}
	return out
}

// String returns a compact, log-friendly representation of hint ranges.
func (h *Hints) String() string {
	if h == nil {
		return "[]"
	}
	return FormatHintRanges(h.TimeRanges)
}

// FormatHintRanges renders ranges as:
// [start +dur];[start +dur];... and truncates to maxLoggedHintRanges.
// Example: [2026-07-09T08:42:59.123Z +1s]
func FormatHintRanges(ranges []HintTimeRange) string {
	if len(ranges) == 0 {
		return "[]"
	}

	limit := min(len(ranges), maxLoggedHintRanges)

	var b strings.Builder
	for i := range limit {
		if i > 0 {
			b.WriteString(";")
		}
		b.WriteString("[")
		if ranges[i].IsPassthrough() {
			// Zero Start is a sentinel, not a real bound — keep End for diagnostics.
			b.WriteString("passthrough,")
			b.WriteString(ranges[i].End.UTC().Format(hintRangeTimeFormat))
		} else {
			start := ranges[i].Start.UTC()
			end := ranges[i].End.UTC()
			dur := end.Sub(start)

			b.WriteString(start.Format(hintRangeTimeFormat))
			b.WriteString(" +")
			b.WriteString(dur.String())
		}
		b.WriteString("]")
	}

	if len(ranges) > limit {
		fmt.Fprintf(&b, "...(+%d more)", len(ranges)-limit)
	}

	return b.String()
}
