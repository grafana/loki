package rules

import (
	"time"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/pkg/value"
	"github.com/prometheus/prometheus/storage"
)

// Cortex-Note: this entire file is the interface and default implementation
// of what was previously defined in prometheus as (*Group).RestoreForState.

// TenantAlertHistory is a tenant specific interface for restoring the for state of an alert after restarts/etc.
type TenantAlertHistory interface {
	RestoreForState(ts time.Time, alertRule *AlertingRule)
	Stop()
}

// QuerierHistory embeds a Querier and determines last active at via the traditional Prometheus metric `ALERTS_FOR_STATE`
type MetricsHistory struct {
	q    storage.Queryable
	opts *ManagerOptions
}

func NewMetricsHistory(q storage.Queryable, opts *ManagerOptions) *MetricsHistory {
	return &MetricsHistory{
		q:    q,
		opts: opts,
	}
}

func (m *MetricsHistory) Stop() {}

func (m *MetricsHistory) RestoreForState(ts time.Time, alertRule *AlertingRule) {
	maxtMS := int64(model.TimeFromUnixNano(ts.UnixNano()))
	// We allow restoration only if alerts were active before after certain time.
	mint := ts.Add(-m.opts.OutageTolerance)
	mintMS := int64(model.TimeFromUnixNano(mint.UnixNano()))
	q, err := m.q.Querier(m.opts.Context, mintMS, maxtMS)
	if err != nil {
		level.Error(m.opts.Logger).Log("msg", "Failed to get Querier", "err", err)
		return
	}
	defer func() {
		if err := q.Close(); err != nil {
			level.Error(m.opts.Logger).Log("msg", "Failed to close Querier", "err", err)
		}
	}()

	alertRule.ForEachActiveAlert(func(a *Alert) {
		smpl := alertRule.ForStateSample(a, time.Now(), 0)
		var matchers []*labels.Matcher
		for _, l := range smpl.Metric {
			mt, err := labels.NewMatcher(labels.MatchEqual, l.Name, l.Value)
			if err != nil {
				panic(err)
			}
			matchers = append(matchers, mt)
		}

		sset := q.Select(false, nil, matchers...)

		seriesFound := false
		var s storage.Series
		for sset.Next() {
			// Query assures that smpl.Metric is included in sset.At().Labels(),
			// hence just checking the length would act like equality.
			// (This is faster than calling labels.Compare again as we already have some info).
			if len(sset.At().Labels()) == len(smpl.Metric) {
				s = sset.At()
				seriesFound = true
				break
			}
		}

		if !seriesFound {
			return
		}

		// Series found for the 'for' state.
		var t int64
		var v float64
		it := s.Iterator()
		for it.Next() {
			t, v = it.At()
		}
		if it.Err() != nil {
			level.Error(m.opts.Logger).Log("msg", "Failed to restore 'for' state",
				labels.AlertName, alertRule.Name(), "stage", "Iterator", "err", it.Err())
			return
		}
		if value.IsStaleNaN(v) { // Alert was not active.
			return
		}

		alertHoldDuration := alertRule.HoldDuration()
		downAt := time.Unix(t/1000, 0)
		restoredActiveAt := time.Unix(int64(v), 0)
		timeSpentPending := downAt.Sub(restoredActiveAt)
		timeRemainingPending := alertHoldDuration - timeSpentPending

		if timeRemainingPending <= 0 {
			// It means that alert was firing when prometheus went down.
			// In the next Eval, the state of this alert will be set back to
			// firing again if it's still firing in that Eval.
			// Nothing to be done in this case.
		} else if timeRemainingPending < m.opts.ForGracePeriod {
			// (new) restoredActiveAt = (ts + m.opts.ForGracePeriod) - alertHoldDuration
			//                            /* new firing time */      /* moving back by hold duration */
			//
			// Proof of correctness:
			// firingTime = restoredActiveAt.Add(alertHoldDuration)
			//            = ts + m.opts.ForGracePeriod - alertHoldDuration + alertHoldDuration
			//            = ts + m.opts.ForGracePeriod
			//
			// Time remaining to fire = firingTime.Sub(ts)
			//                        = (ts + m.opts.ForGracePeriod) - ts
			//                        = m.opts.ForGracePeriod
			restoredActiveAt = ts.Add(m.opts.ForGracePeriod).Add(-alertHoldDuration)
		} else {
			// By shifting ActiveAt to the future (ActiveAt + some_duration),
			// the total pending time from the original ActiveAt
			// would be `alertHoldDuration + some_duration`.
			// Here, some_duration = downDuration.
			downDuration := ts.Sub(downAt)
			restoredActiveAt = restoredActiveAt.Add(downDuration)
		}

		a.ActiveAt = restoredActiveAt
		level.Debug(m.opts.Logger).Log("msg", "'for' state restored",
			labels.AlertName, alertRule.Name(), "restored_time", a.ActiveAt.Format(time.RFC850),
			"labels", a.Labels.String())

	})

	alertRule.SetRestored(true)
}
