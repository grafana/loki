package loki

import (
	"testing"

	"github.com/go-kit/log"
	"github.com/grafana/dskit/kv/memberlist"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/stretchr/testify/require"
)

const memberlistQueueMetric = "loki_memberlist_client_messages_in_broadcast_queue"

// scrapingRegisterer writes each collector as it is registered. That is the
// /metrics scrape that lands inside memberlist.NewKV, without taking the
// registry lock twice (Gather from inside Register deadlocks).
type scrapingRegisterer struct {
	reg *prometheus.Registry
}

func (s scrapingRegisterer) Register(c prometheus.Collector) error {
	if err := s.reg.Register(c); err != nil {
		return err
	}
	scrapeCollector(c)
	return nil
}

func (s scrapingRegisterer) MustRegister(cs ...prometheus.Collector) {
	for _, c := range cs {
		if err := s.Register(c); err != nil {
			panic(err)
		}
	}
}

func (s scrapingRegisterer) Unregister(c prometheus.Collector) bool {
	return s.reg.Unregister(c)
}

func scrapeCollector(c prometheus.Collector) {
	ch := make(chan prometheus.Metric, 1024)
	c.Collect(ch)
	close(ch)
	for m := range ch {
		if err := m.Write(&dto.Metric{}); err != nil {
			panic(err)
		}
	}
}

func testMemberlistKVConfig() memberlist.KVConfig {
	return memberlist.KVConfig{MetricsNamespace: "loki"}
}

func metricValues(t *testing.T, reg *prometheus.Registry, name string) []float64 {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	var values []float64
	for _, family := range families {
		if family.GetName() != name {
			continue
		}
		for _, m := range family.GetMetric() {
			values = append(values, m.GetGauge().GetValue())
		}
	}
	return values
}

func registryHasMetric(t *testing.T, reg *prometheus.Registry, name string) bool {
	t.Helper()
	families, err := reg.Gather()
	require.NoError(t, err)
	for _, family := range families {
		if family.GetName() == name {
			return true
		}
	}
	return false
}

// TestNewKVMemberlistMetricsPanicWhenScrapedDuringInit locks in the dskit bug
// this package works around. NewKV registers State() gauges before it sets
// NamedService, so a scrape of those gauges panics. When a dskit bump fixes
// that order, this test fails and the held registerer can be removed.
func TestNewKVMemberlistMetricsPanicWhenScrapedDuringInit(t *testing.T) {
	reg := scrapingRegisterer{reg: prometheus.NewRegistry()}
	require.Panics(t, func() {
		memberlist.NewKV(testMemberlistKVConfig(), log.NewNopLogger(), nil, reg)
	})
}

func TestHeldRegistererPublishesMemberlistMetricsAfterKVExists(t *testing.T) {
	inner := prometheus.NewRegistry()
	// Scraping on the way out of the hold is what a /metrics scrape does the
	// moment the collectors become visible. That must not panic.
	held := newHeldRegisterer(scrapingRegisterer{reg: inner})

	var kv *memberlist.KV
	require.NotPanics(t, func() {
		kv = memberlist.NewKV(testMemberlistKVConfig(), log.NewNopLogger(), nil, held)
	})
	require.NotNil(t, kv)
	require.False(t, registryHasMetric(t, inner, memberlistQueueMetric), "gauges must stay unpublished while NewKV is still assigning the service")

	require.NotPanics(t, func() {
		held.release()
	})
	// Both panicking callbacks (queue length and cluster member count) are
	// scrapeable once the service exists, and report 0 until it is Running.
	for _, name := range []string{
		memberlistQueueMetric,
		"loki_memberlist_client_cluster_members_count",
		"loki_memberlist_client_cluster_node_health_score",
	} {
		values := metricValues(t, inner, name)
		require.NotEmpty(t, values, name)
		for _, value := range values {
			require.Zero(t, value, name)
		}
	}

	// sync.Once waiters all call release. A second call must not re-register.
	require.NotPanics(t, held.release)
}

func TestHeldRegistererUnregisterDropsPendingCollector(t *testing.T) {
	inner := prometheus.NewRegistry()
	held := newHeldRegisterer(inner)
	gauge := prometheus.NewGauge(prometheus.GaugeOpts{Name: "pending_gauge", Help: "test"})

	held.MustRegister(gauge)
	require.True(t, held.Unregister(gauge))
	require.False(t, held.Unregister(gauge))

	held.release()
	require.False(t, registryHasMetric(t, inner, "pending_gauge"))
}
