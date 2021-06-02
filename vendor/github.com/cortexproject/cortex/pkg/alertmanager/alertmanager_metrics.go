package alertmanager

import (
	"github.com/prometheus/client_golang/prometheus"

	"github.com/cortexproject/cortex/pkg/util"
)

// This struct aggregates metrics exported by Alertmanager
// and re-exports those aggregates as Cortex metrics.
type alertmanagerMetrics struct {
	regs *util.UserRegistries

	// exported metrics, gathered from Alertmanager API
	alertsReceived *prometheus.Desc
	alertsInvalid  *prometheus.Desc

	// exported metrics, gathered from Alertmanager PipelineBuilder
	numNotifications                   *prometheus.Desc
	numFailedNotifications             *prometheus.Desc
	numNotificationRequestsTotal       *prometheus.Desc
	numNotificationRequestsFailedTotal *prometheus.Desc
	notificationLatencySeconds         *prometheus.Desc

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

	// The alertmanager config hash.
	configHashValue *prometheus.Desc

	partialMerges           *prometheus.Desc
	partialMergesFailed     *prometheus.Desc
	replicationTotal        *prometheus.Desc
	replicationFailed       *prometheus.Desc
	fetchReplicaStateTotal  *prometheus.Desc
	fetchReplicaStateFailed *prometheus.Desc
	initialSyncTotal        *prometheus.Desc
	initialSyncCompleted    *prometheus.Desc
	initialSyncDuration     *prometheus.Desc
	persistTotal            *prometheus.Desc
	persistFailed           *prometheus.Desc

	notificationRateLimited *prometheus.Desc
}

func newAlertmanagerMetrics() *alertmanagerMetrics {
	return &alertmanagerMetrics{
		regs: util.NewUserRegistries(),
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
		numNotificationRequestsTotal: prometheus.NewDesc(
			"cortex_alertmanager_notification_requests_total",
			"The total number of attempted notification requests.",
			[]string{"user", "integration"}, nil),
		numNotificationRequestsFailedTotal: prometheus.NewDesc(
			"cortex_alertmanager_notification_requests_failed_total",
			"The total number of failed notification requests.",
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
		configHashValue: prometheus.NewDesc(
			"cortex_alertmanager_config_hash",
			"Hash of the currently loaded alertmanager configuration.",
			[]string{"user"}, nil),
		partialMerges: prometheus.NewDesc(
			"cortex_alertmanager_partial_state_merges_total",
			"Number of times we have received a partial state to merge for a key.",
			[]string{"user"}, nil),
		partialMergesFailed: prometheus.NewDesc(
			"cortex_alertmanager_partial_state_merges_failed_total",
			"Number of times we have failed to merge a partial state received for a key.",
			[]string{"user"}, nil),
		replicationTotal: prometheus.NewDesc(
			"cortex_alertmanager_state_replication_total",
			"Number of times we have tried to replicate a state to other alertmanagers",
			[]string{"user"}, nil),
		replicationFailed: prometheus.NewDesc(
			"cortex_alertmanager_state_replication_failed_total",
			"Number of times we have failed to replicate a state to other alertmanagers",
			[]string{"user"}, nil),
		fetchReplicaStateTotal: prometheus.NewDesc(
			"cortex_alertmanager_state_fetch_replica_state_total",
			"Number of times we have tried to read and merge the full state from another replica.",
			nil, nil),
		fetchReplicaStateFailed: prometheus.NewDesc(
			"cortex_alertmanager_state_fetch_replica_state_failed_total",
			"Number of times we have failed to read and merge the full state from another replica.",
			nil, nil),
		initialSyncTotal: prometheus.NewDesc(
			"cortex_alertmanager_state_initial_sync_total",
			"Number of times we have tried to sync initial state from peers or storage.",
			nil, nil),
		initialSyncCompleted: prometheus.NewDesc(
			"cortex_alertmanager_state_initial_sync_completed_total",
			"Number of times we have completed syncing initial state for each possible outcome.",
			[]string{"outcome"}, nil),
		initialSyncDuration: prometheus.NewDesc(
			"cortex_alertmanager_state_initial_sync_duration_seconds",
			"Time spent syncing initial state from peers or storage.",
			nil, nil),
		persistTotal: prometheus.NewDesc(
			"cortex_alertmanager_state_persist_total",
			"Number of times we have tried to persist the running state to storage.",
			nil, nil),
		persistFailed: prometheus.NewDesc(
			"cortex_alertmanager_state_persist_failed_total",
			"Number of times we have failed to persist the running state to storage.",
			nil, nil),
		notificationRateLimited: prometheus.NewDesc(
			"cortex_alertmanager_notification_rate_limited_total",
			"Total number of rate-limited notifications per integration.",
			[]string{"user", "integration"}, nil),
	}
}

func (m *alertmanagerMetrics) addUserRegistry(user string, reg *prometheus.Registry) {
	m.regs.AddUserRegistry(user, reg)
}

func (m *alertmanagerMetrics) removeUserRegistry(user string) {
	// We need to go for a soft deletion here, as hard deletion requires
	// that _all_ metrics except gauges are per-user.
	m.regs.RemoveUserRegistry(user, false)
}

func (m *alertmanagerMetrics) Describe(out chan<- *prometheus.Desc) {
	out <- m.alertsReceived
	out <- m.alertsInvalid
	out <- m.numNotifications
	out <- m.numFailedNotifications
	out <- m.numNotificationRequestsTotal
	out <- m.numNotificationRequestsFailedTotal
	out <- m.notificationLatencySeconds
	out <- m.markerAlerts
	out <- m.nflogGCDuration
	out <- m.nflogSnapshotDuration
	out <- m.nflogSnapshotSize
	out <- m.nflogQueriesTotal
	out <- m.nflogQueryErrorsTotal
	out <- m.nflogQueryDuration
	out <- m.nflogPropagatedMessagesTotal
	out <- m.silencesGCDuration
	out <- m.silencesSnapshotDuration
	out <- m.silencesSnapshotSize
	out <- m.silencesQueriesTotal
	out <- m.silencesQueryErrorsTotal
	out <- m.silencesQueryDuration
	out <- m.silencesPropagatedMessagesTotal
	out <- m.silences
	out <- m.configHashValue
	out <- m.partialMerges
	out <- m.partialMergesFailed
	out <- m.replicationTotal
	out <- m.replicationFailed
	out <- m.fetchReplicaStateTotal
	out <- m.fetchReplicaStateFailed
	out <- m.initialSyncTotal
	out <- m.initialSyncCompleted
	out <- m.initialSyncDuration
	out <- m.persistTotal
	out <- m.persistFailed
	out <- m.notificationRateLimited
}

func (m *alertmanagerMetrics) Collect(out chan<- prometheus.Metric) {
	data := m.regs.BuildMetricFamiliesPerUser()

	data.SendSumOfCountersPerUser(out, m.alertsReceived, "alertmanager_alerts_received_total")
	data.SendSumOfCountersPerUser(out, m.alertsInvalid, "alertmanager_alerts_invalid_total")

	data.SendSumOfCountersPerUserWithLabels(out, m.numNotifications, "alertmanager_notifications_total", "integration")
	data.SendSumOfCountersPerUserWithLabels(out, m.numFailedNotifications, "alertmanager_notifications_failed_total", "integration")
	data.SendSumOfCountersPerUserWithLabels(out, m.numNotificationRequestsTotal, "alertmanager_notification_requests_total", "integration")
	data.SendSumOfCountersPerUserWithLabels(out, m.numNotificationRequestsFailedTotal, "alertmanager_notification_requests_failed_total", "integration")
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

	data.SendMaxOfGaugesPerUser(out, m.configHashValue, "alertmanager_config_hash")

	data.SendSumOfCountersPerUser(out, m.partialMerges, "alertmanager_partial_state_merges_total")
	data.SendSumOfCountersPerUser(out, m.partialMergesFailed, "alertmanager_partial_state_merges_failed_total")
	data.SendSumOfCountersPerUser(out, m.replicationTotal, "alertmanager_state_replication_total")
	data.SendSumOfCountersPerUser(out, m.replicationFailed, "alertmanager_state_replication_failed_total")
	data.SendSumOfCounters(out, m.fetchReplicaStateTotal, "alertmanager_state_fetch_replica_state_total")
	data.SendSumOfCounters(out, m.fetchReplicaStateFailed, "alertmanager_state_fetch_replica_state_failed_total")
	data.SendSumOfCounters(out, m.initialSyncTotal, "alertmanager_state_initial_sync_total")
	data.SendSumOfCountersWithLabels(out, m.initialSyncCompleted, "alertmanager_state_initial_sync_completed_total", "outcome")
	data.SendSumOfHistograms(out, m.initialSyncDuration, "alertmanager_state_initial_sync_duration_seconds")
	data.SendSumOfCounters(out, m.persistTotal, "alertmanager_state_persist_total")
	data.SendSumOfCounters(out, m.persistFailed, "alertmanager_state_persist_failed_total")

	data.SendSumOfCountersPerUserWithLabels(out, m.notificationRateLimited, "alertmanager_notification_rate_limited_total", "integration")
}
