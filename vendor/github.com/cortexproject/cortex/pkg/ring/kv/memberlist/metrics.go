package memberlist

import (
	"github.com/prometheus/client_golang/prometheus"
)

func (m *Client) createAndRegisterMetrics() {
	const subsystem = "memberlist_client"

	m.numberOfReceivedMessages = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "received_broadcasts",
		Help:      "Number of received broadcast user messages",
	})

	m.totalSizeOfReceivedMessages = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "received_broadcasts_size",
		Help:      "Total size of received broadcast user messages",
	})

	m.numberOfInvalidReceivedMessages = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "received_broadcasts_invalid",
		Help:      "Number of received broadcast user messages that were invalid. Hopefully 0.",
	})

	m.numberOfPushes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "state_pushes_count",
		Help:      "How many times did this node push its full state to another node",
	})

	m.totalSizeOfPushes = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "state_pushes_bytes",
		Help:      "Total size of pushed state",
	})

	m.numberOfPulls = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "state_pulls_count",
		Help:      "How many times did this node pull full state from another node",
	})

	m.totalSizeOfPulls = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "state_pulls_bytes",
		Help:      "Total size of pulled state",
	})

	m.numberOfBroadcastMessagesInQueue = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "messages_in_broadcast_queue",
		Help:      "Number of user messages in the broadcast queue",
	}, func() float64 {
		return float64(m.broadcasts.NumQueued())
	})

	m.totalSizeOfBroadcastMessagesInQueue = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "messages_in_broadcast_size_total",
		Help:      "Total size of messages waiting in the broadcast queue",
	})

	m.casAttempts = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "cas_attempt_count",
		Help:      "Attempted CAS operations",
	})

	m.casSuccesses = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "cas_success_count",
		Help:      "Successful CAS operations",
	})

	m.casFailures = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "cas_failure_count",
		Help:      "Failed CAS operations",
	})

	m.storeValuesDesc = prometheus.NewDesc(
		prometheus.BuildFQName(m.cfg.MetricsNamespace, subsystem, "kv_store_count"),
		"Number of values in KV Store",
		nil, nil,
	)

	m.storeSizesDesc = prometheus.NewDesc(
		prometheus.BuildFQName(m.cfg.MetricsNamespace, subsystem, "kv_store_value_size"),
		"Sizes of values in KV Store in bytes",
		[]string{"key"}, nil)

	m.memberlistMembersCount = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "cluster_members_count",
		Help:      "Number of members in memberlist cluster",
	}, func() float64 {
		return float64(m.memberlist.NumMembers())
	})

	m.memberlistHealthScore = prometheus.NewGaugeFunc(prometheus.GaugeOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "cluster_node_health_score",
		Help:      "Health score of this cluster. Lower the better. 0 = totally health",
	}, func() float64 {
		return float64(m.memberlist.GetHealthScore())
	})

	m.memberlistMembersInfoDesc = prometheus.NewDesc(
		prometheus.BuildFQName(m.cfg.MetricsNamespace, subsystem, "cluster_members_last_timestamp"),
		"State information about memberlist cluster members",
		[]string{"name", "address"}, nil)

	m.watchPrefixDroppedNotifications = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: m.cfg.MetricsNamespace,
		Subsystem: subsystem,
		Name:      "watch_prefix_dropped_notifications",
		Help:      "Number of dropped notifications in WatchPrefix function",
	}, []string{"prefix"})

	if m.cfg.MetricsRegisterer == nil {
		return
	}

	all := []prometheus.Collector{
		m.numberOfReceivedMessages,
		m.totalSizeOfReceivedMessages,
		m.numberOfInvalidReceivedMessages,
		m.numberOfBroadcastMessagesInQueue,
		m.numberOfPushes,
		m.numberOfPulls,
		m.totalSizeOfPushes,
		m.totalSizeOfPulls,
		m.totalSizeOfBroadcastMessagesInQueue,
		m.casAttempts,
		m.casFailures,
		m.casSuccesses,
		m.watchPrefixDroppedNotifications,
		m.memberlistMembersCount,
		m.memberlistHealthScore,
	}

	for _, c := range all {
		m.cfg.MetricsRegisterer.MustRegister(c.(prometheus.Collector))
	}

	m.cfg.MetricsRegisterer.MustRegister(m)
}

// Describe returns prometheus descriptions via supplied channel
func (m *Client) Describe(ch chan<- *prometheus.Desc) {
	ch <- m.storeValuesDesc
	ch <- m.storeSizesDesc
	ch <- m.memberlistMembersInfoDesc
}

// Collect returns extra metrics via supplied channel
func (m *Client) Collect(ch chan<- prometheus.Metric) {
	m.storeMu.Lock()
	defer m.storeMu.Unlock()

	ch <- prometheus.MustNewConstMetric(m.storeValuesDesc, prometheus.GaugeValue, float64(len(m.store)))

	for k, v := range m.store {
		ch <- prometheus.MustNewConstMetric(m.storeSizesDesc, prometheus.GaugeValue, float64(len(v.value)), k)
	}
}
