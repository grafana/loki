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

// plannedQueryRanges distinguishes "absent" from "present, including empty".
// A present empty list means the query should be sized as zero bytes.
type plannedQueryRanges struct {
	ranges []TimeRange
}

// InjectPlannedQueryRanges attaches a finished plan for the size limiter.
// Keep it on the in-process context only; it is not an HTTP or gRPC header.
// A present empty slice means "scan nothing" (0 bytes). Omitting the value
// means size the request range.
func InjectPlannedQueryRanges(ctx context.Context, ranges []TimeRange) context.Context {
	copied := append([]TimeRange(nil), ranges...)
	return context.WithValue(ctx, plannedQueryRangesCtxKey, &plannedQueryRanges{ranges: copied})
}

// ExtractPlannedQueryRanges returns planned ranges and whether they were set.
func ExtractPlannedQueryRanges(ctx context.Context) ([]TimeRange, bool) {
	v, ok := ctx.Value(plannedQueryRangesCtxKey).(*plannedQueryRanges)
	if !ok || v == nil {
		return nil, false
	}
	return v.ranges, true
}
