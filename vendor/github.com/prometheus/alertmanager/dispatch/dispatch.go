// Copyright 2018 Prometheus Team
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package dispatch

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/common/model"

	"github.com/prometheus/alertmanager/notify"
	"github.com/prometheus/alertmanager/provider"
	"github.com/prometheus/alertmanager/store"
	"github.com/prometheus/alertmanager/types"
)

// DispatcherMetrics represents metrics associated to a dispatcher.
type DispatcherMetrics struct {
	aggrGroups         prometheus.Gauge
	processingDuration prometheus.Summary
}

// NewDispatcherMetrics returns a new registered DispatchMetrics.
func NewDispatcherMetrics(r prometheus.Registerer) *DispatcherMetrics {
	m := DispatcherMetrics{
		aggrGroups: prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: "alertmanager_dispatcher_aggregation_groups",
				Help: "Number of active aggregation groups",
			},
		),
		processingDuration: prometheus.NewSummary(
			prometheus.SummaryOpts{
				Name: "alertmanager_dispatcher_alert_processing_duration_seconds",
				Help: "Summary of latencies for the processing of alerts.",
			},
		),
	}

	if r != nil {
		r.MustRegister(m.aggrGroups, m.processingDuration)
	}

	return &m
}

// Dispatcher sorts incoming alerts into aggregation groups and
// assigns the correct notifiers to each.
type Dispatcher struct {
	route   *Route
	alerts  provider.Alerts
	stage   notify.Stage
	metrics *DispatcherMetrics

	marker  types.Marker
	timeout func(time.Duration) time.Duration

	aggrGroups map[*Route]map[model.Fingerprint]*aggrGroup
	mtx        sync.RWMutex

	done   chan struct{}
	ctx    context.Context
	cancel func()

	logger log.Logger
}

// NewDispatcher returns a new Dispatcher.
func NewDispatcher(
	ap provider.Alerts,
	r *Route,
	s notify.Stage,
	mk types.Marker,
	to func(time.Duration) time.Duration,
	l log.Logger,
	m *DispatcherMetrics,
) *Dispatcher {
	disp := &Dispatcher{
		alerts:  ap,
		stage:   s,
		route:   r,
		marker:  mk,
		timeout: to,
		logger:  log.With(l, "component", "dispatcher"),
		metrics: m,
	}
	return disp
}

// Run starts dispatching alerts incoming via the updates channel.
func (d *Dispatcher) Run() {
	d.done = make(chan struct{})

	d.mtx.Lock()
	d.aggrGroups = map[*Route]map[model.Fingerprint]*aggrGroup{}
	d.metrics.aggrGroups.Set(0)
	d.ctx, d.cancel = context.WithCancel(context.Background())
	d.mtx.Unlock()

	d.run(d.alerts.Subscribe())
	close(d.done)
}

func (d *Dispatcher) run(it provider.AlertIterator) {
	cleanup := time.NewTicker(30 * time.Second)
	defer cleanup.Stop()

	defer it.Close()

	for {
		select {
		case alert, ok := <-it.Next():
			if !ok {
				// Iterator exhausted for some reason.
				if err := it.Err(); err != nil {
					level.Error(d.logger).Log("msg", "Error on alert update", "err", err)
				}
				return
			}

			level.Debug(d.logger).Log("msg", "Received alert", "alert", alert)

			// Log errors but keep trying.
			if err := it.Err(); err != nil {
				level.Error(d.logger).Log("msg", "Error on alert update", "err", err)
				continue
			}

			now := time.Now()
			for _, r := range d.route.Match(alert.Labels) {
				d.processAlert(alert, r)
			}
			d.metrics.processingDuration.Observe(time.Since(now).Seconds())

		case <-cleanup.C:
			d.mtx.Lock()

			for _, groups := range d.aggrGroups {
				for _, ag := range groups {
					if ag.empty() {
						ag.stop()
						delete(groups, ag.fingerprint())
						d.metrics.aggrGroups.Dec()
					}
				}
			}

			d.mtx.Unlock()

		case <-d.ctx.Done():
			return
		}
	}
}

// AlertGroup represents how alerts exist within an aggrGroup.
type AlertGroup struct {
	Alerts   types.AlertSlice
	Labels   model.LabelSet
	Receiver string
}

type AlertGroups []*AlertGroup

func (ag AlertGroups) Swap(i, j int) { ag[i], ag[j] = ag[j], ag[i] }
func (ag AlertGroups) Less(i, j int) bool {
	if ag[i].Labels.Equal(ag[j].Labels) {
		return ag[i].Receiver < ag[j].Receiver
	}
	return ag[i].Labels.Before(ag[j].Labels)
}
func (ag AlertGroups) Len() int { return len(ag) }

// Groups returns a slice of AlertGroups from the dispatcher's internal state.
func (d *Dispatcher) Groups(routeFilter func(*Route) bool, alertFilter func(*types.Alert, time.Time) bool) (AlertGroups, map[model.Fingerprint][]string) {
	groups := AlertGroups{}

	d.mtx.RLock()
	defer d.mtx.RUnlock()

	// Keep a list of receivers for an alert to prevent checking each alert
	// again against all routes. The alert has already matched against this
	// route on ingestion.
	receivers := map[model.Fingerprint][]string{}

	now := time.Now()
	for route, ags := range d.aggrGroups {
		if !routeFilter(route) {
			continue
		}

		for _, ag := range ags {
			receiver := route.RouteOpts.Receiver
			alertGroup := &AlertGroup{
				Labels:   ag.labels,
				Receiver: receiver,
			}

			alerts := ag.alerts.List()
			filteredAlerts := make([]*types.Alert, 0, len(alerts))
			for _, a := range alerts {
				if !alertFilter(a, now) {
					continue
				}

				fp := a.Fingerprint()
				if r, ok := receivers[fp]; ok {
					// Receivers slice already exists. Add
					// the current receiver to the slice.
					receivers[fp] = append(r, receiver)
				} else {
					// First time we've seen this alert fingerprint.
					// Initialize a new receivers slice.
					receivers[fp] = []string{receiver}
				}

				filteredAlerts = append(filteredAlerts, a)
			}
			if len(filteredAlerts) == 0 {
				continue
			}
			alertGroup.Alerts = filteredAlerts

			groups = append(groups, alertGroup)
		}
	}
	sort.Sort(groups)
	for i := range groups {
		sort.Sort(groups[i].Alerts)
	}
	for i := range receivers {
		sort.Strings(receivers[i])
	}

	return groups, receivers
}

// Stop the dispatcher.
func (d *Dispatcher) Stop() {
	if d == nil {
		return
	}
	d.mtx.Lock()
	if d.cancel == nil {
		return
	}
	d.cancel()
	d.cancel = nil
	d.mtx.Unlock()

	<-d.done
}

// notifyFunc is a function that performs notification for the alert
// with the given fingerprint. It aborts on context cancelation.
// Returns false iff notifying failed.
type notifyFunc func(context.Context, ...*types.Alert) bool

// processAlert determines in which aggregation group the alert falls
// and inserts it.
func (d *Dispatcher) processAlert(alert *types.Alert, route *Route) {
	groupLabels := getGroupLabels(alert, route)

	fp := groupLabels.Fingerprint()

	d.mtx.Lock()
	defer d.mtx.Unlock()

	group, ok := d.aggrGroups[route]
	if !ok {
		group = map[model.Fingerprint]*aggrGroup{}
		d.aggrGroups[route] = group
	}

	// If the group does not exist, create it.
	ag, ok := group[fp]
	if !ok {
		ag = newAggrGroup(d.ctx, groupLabels, route, d.timeout, d.logger)
		group[fp] = ag
		d.metrics.aggrGroups.Inc()

		go ag.run(func(ctx context.Context, alerts ...*types.Alert) bool {
			_, _, err := d.stage.Exec(ctx, d.logger, alerts...)
			if err != nil {
				lvl := level.Error(d.logger)
				if ctx.Err() == context.Canceled {
					// It is expected for the context to be canceled on
					// configuration reload or shutdown. In this case, the
					// message should only be logged at the debug level.
					lvl = level.Debug(d.logger)
				}
				lvl.Log("msg", "Notify for alerts failed", "num_alerts", len(alerts), "err", err)
			}
			return err == nil
		})
	}

	ag.insert(alert)
}

func getGroupLabels(alert *types.Alert, route *Route) model.LabelSet {
	groupLabels := model.LabelSet{}
	for ln, lv := range alert.Labels {
		if _, ok := route.RouteOpts.GroupBy[ln]; ok || route.RouteOpts.GroupByAll {
			groupLabels[ln] = lv
		}
	}

	return groupLabels
}

// aggrGroup aggregates alert fingerprints into groups to which a
// common set of routing options applies.
// It emits notifications in the specified intervals.
type aggrGroup struct {
	labels   model.LabelSet
	opts     *RouteOpts
	logger   log.Logger
	routeKey string

	alerts  *store.Alerts
	ctx     context.Context
	cancel  func()
	done    chan struct{}
	next    *time.Timer
	timeout func(time.Duration) time.Duration

	mtx        sync.RWMutex
	hasFlushed bool
}

// newAggrGroup returns a new aggregation group.
func newAggrGroup(ctx context.Context, labels model.LabelSet, r *Route, to func(time.Duration) time.Duration, logger log.Logger) *aggrGroup {
	if to == nil {
		to = func(d time.Duration) time.Duration { return d }
	}
	ag := &aggrGroup{
		labels:   labels,
		routeKey: r.Key(),
		opts:     &r.RouteOpts,
		timeout:  to,
		alerts:   store.NewAlerts(),
		done:     make(chan struct{}),
	}
	ag.ctx, ag.cancel = context.WithCancel(ctx)

	ag.logger = log.With(logger, "aggrGroup", ag)

	// Set an initial one-time wait before flushing
	// the first batch of notifications.
	ag.next = time.NewTimer(ag.opts.GroupWait)

	return ag
}

func (ag *aggrGroup) fingerprint() model.Fingerprint {
	return ag.labels.Fingerprint()
}

func (ag *aggrGroup) GroupKey() string {
	return fmt.Sprintf("%s:%s", ag.routeKey, ag.labels)
}

func (ag *aggrGroup) String() string {
	return ag.GroupKey()
}

func (ag *aggrGroup) run(nf notifyFunc) {
	defer close(ag.done)
	defer ag.next.Stop()

	for {
		select {
		case now := <-ag.next.C:
			// Give the notifications time until the next flush to
			// finish before terminating them.
			ctx, cancel := context.WithTimeout(ag.ctx, ag.timeout(ag.opts.GroupInterval))

			// The now time we retrieve from the ticker is the only reliable
			// point of time reference for the subsequent notification pipeline.
			// Calculating the current time directly is prone to flaky behavior,
			// which usually only becomes apparent in tests.
			ctx = notify.WithNow(ctx, now)

			// Populate context with information needed along the pipeline.
			ctx = notify.WithGroupKey(ctx, ag.GroupKey())
			ctx = notify.WithGroupLabels(ctx, ag.labels)
			ctx = notify.WithReceiverName(ctx, ag.opts.Receiver)
			ctx = notify.WithRepeatInterval(ctx, ag.opts.RepeatInterval)
			ctx = notify.WithMuteTimeIntervals(ctx, ag.opts.MuteTimeIntervals)

			// Wait the configured interval before calling flush again.
			ag.mtx.Lock()
			ag.next.Reset(ag.opts.GroupInterval)
			ag.hasFlushed = true
			ag.mtx.Unlock()

			ag.flush(func(alerts ...*types.Alert) bool {
				return nf(ctx, alerts...)
			})

			cancel()

		case <-ag.ctx.Done():
			return
		}
	}
}

func (ag *aggrGroup) stop() {
	// Calling cancel will terminate all in-process notifications
	// and the run() loop.
	ag.cancel()
	<-ag.done
}

// insert inserts the alert into the aggregation group.
func (ag *aggrGroup) insert(alert *types.Alert) {
	if err := ag.alerts.Set(alert); err != nil {
		level.Error(ag.logger).Log("msg", "error on set alert", "err", err)
	}

	// Immediately trigger a flush if the wait duration for this
	// alert is already over.
	ag.mtx.Lock()
	defer ag.mtx.Unlock()
	if !ag.hasFlushed && alert.StartsAt.Add(ag.opts.GroupWait).Before(time.Now()) {
		ag.next.Reset(0)
	}
}

func (ag *aggrGroup) empty() bool {
	return ag.alerts.Empty()
}

// flush sends notifications for all new alerts.
func (ag *aggrGroup) flush(notify func(...*types.Alert) bool) {
	if ag.empty() {
		return
	}

	var (
		alerts      = ag.alerts.List()
		alertsSlice = make(types.AlertSlice, 0, len(alerts))
		now         = time.Now()
	)
	for _, alert := range alerts {
		a := *alert
		// Ensure that alerts don't resolve as time move forwards.
		if !a.ResolvedAt(now) {
			a.EndsAt = time.Time{}
		}
		alertsSlice = append(alertsSlice, &a)
	}
	sort.Stable(alertsSlice)

	level.Debug(ag.logger).Log("msg", "flushing", "alerts", fmt.Sprintf("%v", alertsSlice))

	if notify(alertsSlice...) {
		for _, a := range alertsSlice {
			// Only delete if the fingerprint has not been inserted
			// again since we notified about it.
			fp := a.Fingerprint()
			got, err := ag.alerts.Get(fp)
			if err != nil {
				// This should never happen.
				level.Error(ag.logger).Log("msg", "failed to get alert", "err", err, "alert", a.String())
				continue
			}
			if a.Resolved() && got.UpdatedAt == a.UpdatedAt {
				if err := ag.alerts.Delete(fp); err != nil {
					level.Error(ag.logger).Log("msg", "error on delete alert", "err", err, "alert", a.String())
				}
			}
		}
	}
}
