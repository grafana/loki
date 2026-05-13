package metric

import (
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestHighWatermarkGauge(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := NewHighWatermarkGauge("test", "A test metric from the max sample collector.")
		reg := prometheus.NewRegistry()
		require.NoError(t, reg.Register(g))

		require.Equal(t, int64(0), g.val.Load())
		require.Equal(t, int64(0), g.maxVal.Load())

		// Add should update both val and maxVal.
		g.Add(1)
		require.Equal(t, int64(1), g.val.Load())
		require.Equal(t, int64(1), g.maxVal.Load())

		// A second add should update both once more.
		g.Add(2)
		require.Equal(t, int64(3), g.val.Load())
		require.Equal(t, int64(3), g.maxVal.Load())

		// Sub should update val, but not maxVal (as it cannot create a new max).
		g.Sub(2)
		require.Equal(t, int64(1), g.val.Load())
		require.Equal(t, int64(3), g.maxVal.Load())

		// Scrape the metric — should report maxVal.
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP test A test metric from the max sample collector.
# TYPE test gauge
test 3
`), "test"))

		// Second scrape the metric — should report the same maxVal.
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP test A test metric from the max sample collector.
# TYPE test gauge
test 3
`), "test"))

		// After 2 minutes, maxVal should be reset.
		time.Sleep(2 * time.Minute)

		// A subsequent scrape reports the current val.
		require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP test A test metric from the max sample collector.
# TYPE test gauge
test 1
`), "test"))
		require.Equal(t, int64(1), g.val.Load())
		require.Equal(t, int64(1), g.maxVal.Load())
	})
}

func TestHighWatermarkGauge_Add_Sub(t *testing.T) {
	g := NewHighWatermarkGauge("test", "")
	require.Equal(t, int64(0), g.val.Load())
	g.Add(2)
	require.Equal(t, int64(2), g.val.Load())
	require.Equal(t, int64(2), g.maxVal.Load())
	g.Sub(1)
	require.Equal(t, int64(1), g.val.Load())
	require.Equal(t, int64(2), g.maxVal.Load())
}

func TestHighWatermarkGauge_Inc_Dec(t *testing.T) {
	g := NewHighWatermarkGauge("test", "")
	require.Equal(t, int64(0), g.val.Load())
	g.Inc()
	require.Equal(t, int64(1), g.val.Load())
	require.Equal(t, int64(1), g.maxVal.Load())
	g.Inc()
	require.Equal(t, int64(2), g.val.Load())
	require.Equal(t, int64(2), g.maxVal.Load())
	g.Dec()
	require.Equal(t, int64(1), g.val.Load())
	require.Equal(t, int64(2), g.maxVal.Load())
}

func ExampleHighWatermarkGauge() {
	c := NewHighWatermarkGauge("metric_name", "This is the metric description, or help message.")
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	// The metric_name metric will be scraped via /metrics.
}
