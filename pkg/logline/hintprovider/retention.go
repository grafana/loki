package hintprovider

import (
	"context"
	"time"

	"github.com/prometheus/common/model"

	"github.com/grafana/loki/v3/pkg/logline/limits"
	"github.com/grafana/loki/v3/pkg/logline/needle"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
)

// HintSourcePastRetention marks synthetic diagnostic provenance for ranges
// representing query windows before the index retention cutoff. Like
// HintSourcePreMinDate it is not part of cache payloads, so it must not be
// used for required behavior.
const HintSourcePastRetention = "past_retention"

// RetentionHintProvider passes through the part of a query older than the
// retention the scheduler deletes indexes by.
//
// The scheduler deletes an index once its newest log is past that retention,
// but Loki can still return the data: it keeps backfilled chunks by ingestion
// time, and per-stream retention can be longer. A lookup there finds no index
// and would skip the data, so the range [0, cutoff) is added as a passthrough
// and only the rest is looked up.
//
// It composes with min_date: the delegate still adds its own pre-min-date
// passthrough, and the union covers whichever cutoff is later. It wraps the
// hint cache, so the cutoff moving each day does not change cache keys.
type RetentionHintProvider struct {
	delegate  QueryHintProvider
	retention limits.Limits
	now       func() time.Time
}

// NewRetentionHintProvider wraps delegate. A nil retention, or one that
// reports no retention period, leaves the delegate's hints unchanged.
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
	// Through is inclusive, so a query ending exactly at the cutoff still
	// needs the delegate for that instant.
	if through.Time().Before(cutoff) {
		return &Hints{TimeRanges: []HintTimeRange{past}}, NewQueryStats(), nil
	}

	hints, stats, err := p.delegate.ProvideHints(ctx, tenant, expr, model.TimeFromUnixNano(cutoff.UnixNano()), through)
	if err != nil {
		return hints, stats, err
	}
	var ranges []HintTimeRange
	if hints != nil {
		ranges = hints.TimeRanges
	}
	return &Hints{TimeRanges: NormalizeRanges(append([]HintTimeRange{past}, ranges...))}, stats, nil
}

// MinDate returns the delegate's minimum trusted index date, or zero when it
// reports none. The retention cutoff is not part of it.
func (p *RetentionHintProvider) MinDate() time.Time {
	if mdp, ok := p.delegate.(minDateProvider); ok {
		return mdp.MinDate()
	}
	return time.Time{}
}

// Needles returns the delegate's needles, so the frontend plans with the same
// ones when it wraps this provider.
func (p *RetentionHintProvider) Needles(expr syntax.Expr) (needle.Conjunctions, int) {
	if np, ok := p.delegate.(needlesProvider); ok {
		return np.Needles(expr)
	}
	return nil, 0
}

// cutoff returns now minus the retention period, rounded up to the next UTC
// day for clock skew between the scheduler and the frontend, or zero when
// indexes are kept forever.
func (p *RetentionHintProvider) cutoff() time.Time {
	if p.retention == nil {
		return time.Time{}
	}
	period := p.retention.RetentionPeriod()
	if period <= 0 {
		return time.Time{}
	}
	cutoff := p.now().UTC().Add(-period)
	day := truncateToUTCDay(cutoff)
	if day.Before(cutoff) {
		day = day.Add(24 * time.Hour)
	}
	return day
}

var _ QueryHintProvider = (*RetentionHintProvider)(nil)
