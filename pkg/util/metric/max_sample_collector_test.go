package metric

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestMaxSampleCollector(t *testing.T) {
	c := NewMaxSampleCollector("test", "A test metric from the max sample collector.")
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(c))

	require.Equal(t, int64(0), c.val.Load())
	require.Equal(t, int64(0), c.maxVal.Load())

	// Add should update both val and maxVal.
	c.Add(1)
	require.Equal(t, int64(1), c.val.Load())
	require.Equal(t, int64(1), c.maxVal.Load())

	// A second add should update both once more.
	c.Add(2)
	require.Equal(t, int64(3), c.val.Load())
	require.Equal(t, int64(3), c.maxVal.Load())

	// Sub should update val, but not maxVal (as it cannot create a new max).
	c.Sub(2)
	require.Equal(t, int64(1), c.val.Load())
	require.Equal(t, int64(3), c.maxVal.Load())

	// Scrape the metric — should report the max.
	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP test A test metric from the max sample collector.
# TYPE test gauge
test 3
`), "test"))

	// After scrape, maxVal is reset to the current val.
	require.Equal(t, int64(1), c.val.Load())
	require.Equal(t, int64(1), c.maxVal.Load())

	// A subsequent scrape reports the current val.
	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP test A test metric from the max sample collector.
# TYPE test gauge
test 1
`), "test"))
}

func TestMaxSampleCollector_Add_Sub(t *testing.T) {
	c := NewMaxSampleCollector("test", "")
	require.Equal(t, int64(0), c.val.Load())
	c.Add(2)
	require.Equal(t, int64(2), c.val.Load())
	require.Equal(t, int64(2), c.maxVal.Load())
	c.Sub(1)
	require.Equal(t, int64(1), c.val.Load())
	require.Equal(t, int64(2), c.maxVal.Load())
}

func TestMaxSampleCollector_Inc_Dec(t *testing.T) {
	c := NewMaxSampleCollector("test", "")
	require.Equal(t, int64(0), c.val.Load())
	c.Inc()
	require.Equal(t, int64(1), c.val.Load())
	require.Equal(t, int64(1), c.maxVal.Load())
	c.Inc()
	require.Equal(t, int64(2), c.val.Load())
	require.Equal(t, int64(2), c.maxVal.Load())
	c.Dec()
	require.Equal(t, int64(1), c.val.Load())
	require.Equal(t, int64(2), c.maxVal.Load())
}

func ExampleMaxSampleCollector() {
	c := NewMaxSampleCollector("metric_name", "This is the metric description, or help message.")
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	// The metric_name metric will be scraped via /metrics.
}
