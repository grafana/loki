package querylimits

import (
	"context"
	"time"
)

// TimeRange is a half-open [Start, End) window used to size a query.
type TimeRange struct {
	Start time.Time
	End   time.Time
}

// Intersect returns the overlapping half-open window of r and other.
// ok is false when the ranges do not overlap or either side is empty.
func (r TimeRange) Intersect(other TimeRange) (TimeRange, bool) {
	start := r.Start
	if other.Start.After(start) {
		start = other.Start
	}
	end := r.End
	if other.End.Before(end) {
		end = other.End
	}
	if !start.Before(end) {
		return TimeRange{}, false
	}
	return TimeRange{Start: start, End: end}, true
}

// plannedQueryRanges distinguishes "absent" from "present, including empty".
// A present empty list means the query should be sized as zero bytes.
type plannedQueryRanges struct {
	ranges []TimeRange
}

// InjectPlannedQueryRanges attaches in-process planned ranges for the query
// size limiter. This is server-only: do not serialize it onto HTTP or gRPC
// headers. A present empty slice means "scan nothing" (0 bytes). Omitting
// the value means the limiter should fall back to the request range.
func InjectPlannedQueryRanges(ctx context.Context, ranges []TimeRange) context.Context {
	copied := append([]TimeRange(nil), ranges...)
	return context.WithValue(ctx, plannedQueryRangesCtxKey, &plannedQueryRanges{ranges: copied})
}

// ExtractPlannedQueryRanges returns planned ranges and whether they were set.
// ok is false when no server middleware attached a plan.
func ExtractPlannedQueryRanges(ctx context.Context) ([]TimeRange, bool) {
	v, ok := ctx.Value(plannedQueryRangesCtxKey).(*plannedQueryRanges)
	if !ok || v == nil {
		return nil, false
	}
	return v.ranges, true
}
