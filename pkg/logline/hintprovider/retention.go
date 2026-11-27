package hintprovider

import (
	"context"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logline/limits"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// HintSourcePastRetention marks synthetic diagnostic provenance for ranges
// before the index retention cutoff. Like HintSourcePreMinDate it is not part
// of cache payloads, so it must not be used for required behavior.
const HintSourcePastRetention = "past_retention"

// RetentionHintProvider passes through the part of a query older than the
// retention the scheduler deletes indexes by. Loki can still return that data
// (backfilled chunks, longer per-stream retention), but a lookup would find no
// index and skip it. It unions the delegate's hints with [0, cutoff) and only
// asks the delegate about the rest. It wraps the hint cache, so the moving
// cutoff never changes cache keys.
type RetentionHintProvider struct {
	delegate  QueryHintProvider
	retention limits.Limits
	now       func() time.Time
}

// NewRetentionHintProvider wraps delegate. A nil retention, or one without a
// period, leaves the delegate's hints unchanged.
func NewRetentionHintProvider(delegate QueryHintProvider, retention limits.Limits) *RetentionHintProvider {
	return &RetentionHintProvider{delegate: delegate, retention: retention, now: time.Now}
}

// ProvideHints implements QueryHintProvider.
func (p *RetentionHintProvider) ProvideHints(
	ctx context.Context,
	tenant string,
	expr syntax.Expr,
	from, through model.Time,
) (*Hints, *QueryStats, error) {
	cutoff := p.cutoff()
	if cutoff.IsZero() || !from.Time().Before(cutoff) {
		return p.delegate.ProvideHints(ctx, tenant, expr, from, through)
	}

	past := HintTimeRange{End: cutoff, Source: HintSourcePastRetention}
	// Through is inclusive, so a query ending at the cutoff still needs the
	// delegate for that instant.
	if through.Time().Before(cutoff) {
		return &Hints{TimeRanges: []HintTimeRange{past}}, NewQueryStats(), nil
	}

	hints, stats, err := p.delegate.ProvideHints(ctx, tenant, expr, model.TimeFromUnixNano(cutoff.UnixNano()), through)
	if err != nil {
		return hints, stats, err
	}
	ranges := []HintTimeRange{past}
	if hints != nil {
		ranges = append(ranges, hints.TimeRanges...)
	}
	return &Hints{TimeRanges: NormalizeRanges(ranges)}, stats, nil
}

// MinDate returns the delegate's, so the frontend keeps its min_date handling.
func (p *RetentionHintProvider) MinDate() time.Time {
	if mdp, ok := p.delegate.(minDateProvider); ok {
		return mdp.MinDate()
	}
	return time.Time{}
}

// cutoff returns now minus the retention period, rounded up to the next UTC
// day for clock skew with the scheduler, or zero when indexes are kept forever.
func (p *RetentionHintProvider) cutoff() time.Time {
	if p.retention == nil || p.retention.RetentionPeriod() <= 0 {
		return time.Time{}
	}
	cutoff := p.now().UTC().Add(-p.retention.RetentionPeriod())
	if day := truncateToUTCDay(cutoff); day.Before(cutoff) {
		return day.Add(24 * time.Hour)
	}
	return cutoff
}

var _ QueryHintProvider = (*RetentionHintProvider)(nil)
