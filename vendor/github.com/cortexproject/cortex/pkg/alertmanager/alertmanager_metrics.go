package alertmanager

import (
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/util"
)

// This struct aggregates metrics exported by Alertmanager
// and re-exports those aggregates as Cortex metrics.
type alertmanagerMetrics struct {
	// Maps userID -> registry
	regsMu sync.Mutex
	regs   map[string]*prometheus.Registry

	// exported metrics, gathered from Alertmanager API
	alertsReceived *prometheus.Desc
	alertsInvalid  *prometheus.Desc

	// exported metrics, gathered from Alertmanager PipelineBuilder
	numNotifications           *prometheus.Desc
	numFailedNotifications     *prometheus.Desc
	notificationLatencySeconds *prometheus.Desc

	// exported metrics, gathered from Alertmanager nflog
	nflogGCDuration              *prometheus.Desc
	nflogSnapshotDuration        *prometheus.Desc
	nflogSnapshotSize            *prometheus.Desc
	nflogQueriesTotal            *prometheus.Desc
	nflogQueryErrorsTotal        *prometheus.Desc
	nflogQueryDuration           *prometheus.Desc
	nflogPropagatedMessagesTotal *prometheus.Desc

	// exported metrics, gathered from Alertmanager Marker
	markerAlerts *prometheus.Desc

	// exported metrics, gathered from Alertmanager Silences
	silencesGCDuration              *prometheus.Desc
	silencesSnapshotDuration        *prometheus.Desc
	silencesSnapshotSize            *prometheus.Desc
	silencesQueriesTotal            *prometheus.Desc
	silencesQueryErrorsTotal        *prometheus.Desc
	silencesQueryDuration           *prometheus.Desc
	silences                        *prometheus.Desc
	silencesPropagatedMessagesTotal *prometheus.Desc
}

func newAlertmanagerMetrics() *alertmanagerMetrics {
	return &alertmanagerMetrics{
		regs:   map[string]*prometheus.Registry{},
		regsMu: sync.Mutex{},
		alertsReceived: prometheus.NewDesc(
			"cortex_alertmanager_alerts_received_total",
			"The total number of received alerts.",
			[]string{"user"}, nil),
		alertsInvalid: prometheus.NewDesc(
			"cortex_alertmanager_alerts_invalid_total",
			"The total number of received alerts that were invalid.",
			[]string{"user"}, nil),
		numNotifications: prometheus.NewDesc(
			"cortex_alertmanager_notifications_total",
			"The total number of attempted notifications.",
			[]string{"user", "integration"}, nil),
		numFailedNotifications: prometheus.NewDesc(
			"cortex_alertmanager_notifications_failed_total",
			"The total number of failed notifications.",
			[]string{"user", "integration"}, nil),
		notificationLatencySeconds: prometheus.NewDesc(
			"cortex_alertmanager_notification_latency_seconds",
			"The latency of notifications in seconds.",
			nil, nil),
		nflogGCDuration: prometheus.NewDesc(
			"cortex_alertmanager_nflog_gc_duration_seconds",
			"Duration of the last notification log garbage collection cycle.",
			nil, nil),
		nflogSnapshotDuration: prometheus.NewDesc(
			"cortex_alertmanager_nflog_snapshot_duration_seconds",
			"Duration of the last notification log snapshot.",
			nil, nil),
		nflogSnapshotSize: prometheus.NewDesc(
			"cortex_alertmanager_nflog_snapshot_size_bytes",
			"Size of the last notification log snapshot in bytes.",
			nil, nil),
		nflogQueriesTotal: prometheus.NewDesc(
			"cortex_alertmanager_nflog_queries_total",
			"Number of notification log queries were received.",
			nil, nil),
		nflogQueryErrorsTotal: prometheus.NewDesc(
			"cortex_alertmanager_nflog_query_errors_total",
			"Number notification log received queries that failed.",
			nil, nil),
		nflogQueryDuration: prometheus.NewDesc(
			"cortex_alertmanager_nflog_query_duration_seconds",
			"Duration of notification log query evaluation.",
			nil, nil),
		nflogPropagatedMessagesTotal: prometheus.NewDesc(
			"cortex_alertmanager_nflog_gossip_messages_propagated_total",
			"Number of received gossip messages that have been further gossiped.",
			nil, nil),
		markerAlerts: prometheus.NewDesc(
			"cortex_alertmanager_alerts",
			"How many alerts by state.",
			[]string{"user", "state"}, nil),
		silencesGCDuration: prometheus.NewDesc(
			"cortex_alertmanager_silences_gc_duration_seconds",
			"Duration of the last silence garbage collection cycle.",
			nil, nil),
		silencesSnapshotDuration: prometheus.NewDesc(
			"cortex_alertmanager_silences_snapshot_duration_seconds",
			"Duration of the last silence snapshot.",
			nil, nil),
		silencesSnapshotSize: prometheus.NewDesc(
			"cortex_alertmanager_silences_snapshot_size_bytes",
			"Size of the last silence snapshot in bytes.",
			nil, nil),
		silencesQueriesTotal: prometheus.NewDesc(
			"cortex_alertmanager_silences_queries_total",
			"How many silence queries were received.",
			nil, nil),
		silencesQueryErrorsTotal: prometheus.NewDesc(
			"cortex_alertmanager_silences_query_errors_total",
			"How many silence received queries did not succeed.",
			nil, nil),
		silencesQueryDuration: prometheus.NewDesc(
			"cortex_alertmanager_silences_query_duration_seconds",
			"Duration of silence query evaluation.",
			nil, nil),
		silencesPropagatedMessagesTotal: prometheus.NewDesc(
			"cortex_alertmanager_silences_gossip_messages_propagated_total",
			"Number of received gossip messages that have been further gossiped.",
			nil, nil),
		silences: prometheus.NewDesc(
			"cortex_alertmanager_silences",
			"How many silences by state.",
			[]string{"user", "state"}, nil),
	}
}

func (m *alertmanagerMetrics) addUserRegistry(user string, reg *prometheus.Registry) {
	m.regsMu.Lock()
	m.regs[user] = reg
	m.regsMu.Unlock()
}

func (m *alertmanagerMetrics) registries() map[string]*prometheus.Registry {
	regs := map[string]*prometheus.Registry{}

	m.regsMu.Lock()
	defer m.regsMu.Unlock()
	for uid, r := range m.regs {
		regs[uid] = r
	}

	return regs
}

func (m *alertmanagerMetrics) Describe(out chan<- *prometheus.Desc) {
	out <- m.alertsReceived
	out <- m.alertsInvalid
	out <- m.numNotifications
	out <- m.numFailedNotifications
	out <- m.notificationLatencySeconds
	out <- m.nflogGCDuration
	out <- m.nflogSnapshotDuration
	out <- m.nflogSnapshotSize
	out <- m.nflogQueriesTotal
	out <- m.nflogQueryErrorsTotal
	out <- m.nflogQueryDuration
	out <- m.nflogPropagatedMessagesTotal
	out <- m.markerAlerts
	out <- m.silencesGCDuration
	out <- m.silencesSnapshotDuration
	out <- m.silencesSnapshotSize
	out <- m.silencesQueriesTotal
	out <- m.silencesQueryErrorsTotal
	out <- m.silencesQueryDuration
	out <- m.silences
	out <- m.silencesPropagatedMessagesTotal
}

func (m *alertmanagerMetrics) Collect(out chan<- prometheus.Metric) {
	data := util.BuildMetricFamiliesPerUserFromUserRegistries(m.registries())

	data.SendSumOfCountersPerUser(out, m.alertsReceived, "alertmanager_alerts_received_total")
	data.SendSumOfCountersPerUser(out, m.alertsInvalid, "alertmanager_alerts_invalid_total")

	data.SendSumOfCountersPerUserWithLabels(out, m.numNotifications, "alertmanager_notifications_total", "integration")
	data.SendSumOfCountersPerUserWithLabels(out, m.numFailedNotifications, "alertmanager_notifications_failed_total", "integration")
	data.SendSumOfHistograms(out, m.notificationLatencySeconds, "alertmanager_notification_latency_seconds")
	data.SendSumOfGaugesPerUserWithLabels(out, m.markerAlerts, "alertmanager_alerts", "state")

	data.SendSumOfSummaries(out, m.nflogGCDuration, "alertmanager_nflog_gc_duration_seconds")
	data.SendSumOfSummaries(out, m.nflogSnapshotDuration, "alertmanager_nflog_snapshot_duration_seconds")
	data.SendSumOfGauges(out, m.nflogSnapshotSize, "alertmanager_nflog_snapshot_size_bytes")
	data.SendSumOfCounters(out, m.nflogQueriesTotal, "alertmanager_nflog_queries_total")
	data.SendSumOfCounters(out, m.nflogQueryErrorsTotal, "alertmanager_nflog_query_errors_total")
	data.SendSumOfHistograms(out, m.nflogQueryDuration, "alertmanager_nflog_query_duration_seconds")
	data.SendSumOfCounters(out, m.nflogPropagatedMessagesTotal, "alertmanager_nflog_gossip_messages_propagated_total")

	data.SendSumOfSummaries(out, m.silencesGCDuration, "alertmanager_silences_gc_duration_seconds")
	data.SendSumOfSummaries(out, m.silencesSnapshotDuration, "alertmanager_silences_snapshot_duration_seconds")
	data.SendSumOfGauges(out, m.silencesSnapshotSize, "alertmanager_silences_snapshot_size_bytes")
	data.SendSumOfCounters(out, m.silencesQueriesTotal, "alertmanager_silences_queries_total")
	data.SendSumOfCounters(out, m.silencesQueryErrorsTotal, "alertmanager_silences_query_errors_total")
	data.SendSumOfHistograms(out, m.silencesQueryDuration, "alertmanager_silences_query_duration_seconds")
	data.SendSumOfCounters(out, m.silencesPropagatedMessagesTotal, "alertmanager_silences_gossip_messages_propagated_total")
	data.SendSumOfGaugesPerUserWithLabels(out, m.silences, "alertmanager_silences", "state")
}
