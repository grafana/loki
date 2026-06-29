package push

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// HTTPHeaderBackfillDayKey is the HTTP header set by a backfill worker to mark a push as backfill
// data for a given day. When present and valid, Loki adds the internal labels constants.BackfillLabel
// and constants.BackfillDayLabel to every stream in the request.
const HTTPHeaderBackfillDayKey = "X-Loki-Backfill-Day"

// backfillDayLayout is the required format for the X-Loki-Backfill-Day header value.
const backfillDayLayout = "2006-01-02"

// backfillDayContextKey is used as a key for context values to avoid collisions.
type backfillDayContextKey int

const backfillDayKey backfillDayContextKey = 1

// ExtractAndValidateBackfillDay reads the X-Loki-Backfill-Day header from r.
// It returns ("", false, nil) when the header is absent or empty. When the header is set it must be
// a valid YYYY-MM-DD date; otherwise it returns an error so the push is rejected with HTTP 400.
func ExtractAndValidateBackfillDay(r *http.Request) (string, bool, error) {
	day := r.Header.Get(HTTPHeaderBackfillDayKey)
	if day == "" {
		return "", false, nil
	}
	if _, err := time.Parse(backfillDayLayout, day); err != nil {
		return "", false, fmt.Errorf("invalid %s header %q: must be a date in YYYY-MM-DD format", HTTPHeaderBackfillDayKey, day)
	}
	return day, true, nil
}

// InjectBackfillDayContext returns a derived context carrying the validated backfill day.
func InjectBackfillDayContext(ctx context.Context, day string) context.Context {
	return context.WithValue(ctx, backfillDayKey, day)
}

// ExtractBackfillDayContext returns the backfill day stored in ctx, or "" if none is set.
func ExtractBackfillDayContext(ctx context.Context) string {
	day, ok := ctx.Value(backfillDayKey).(string)
	if !ok {
		return ""
	}
	return day
}
