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
	// The collector should have no samples.
	expected := [60]int64{}
	require.Equal(t, expected, c.samples)
	// Inc should update the current value, but not samples, since the
	// timer hasn't fired.
	c.Add(1)
	require.Equal(t, int64(1), c.val.Load())
	require.Equal(t, int64(0), c.maxVal.Load())
	require.Equal(t, expected, c.samples)
	// Call the iterFunc to mimic the timer.
	require.NoError(t, c.iterFunc(t.Context()))
	require.Equal(t, int64(1), c.val.Load())
	require.Equal(t, int64(1), c.maxVal.Load())
	expected[0] = 1
	require.Equal(t, expected, c.samples)
	// Call the iterFunc once more, the next sample should also be 1.
	require.NoError(t, c.iterFunc(t.Context()))
	expected[1] = 1
	require.Equal(t, expected, c.samples)
	require.Equal(t, int64(1), c.val.Load())
	require.Equal(t, int64(1), c.maxVal.Load())
	// Inc one last time, and call the iterFunc.
	c.Add(2)
	require.Equal(t, int64(3), c.val.Load())
	require.Equal(t, int64(1), c.maxVal.Load())
	require.NoError(t, c.iterFunc(t.Context()))
	expected[2] = 3
	require.Equal(t, expected, c.samples)
	require.Equal(t, int64(3), c.val.Load())
	require.Equal(t, int64(3), c.maxVal.Load())
	// Scrape the metric.
	reg := prometheus.NewRegistry()
	require.NoError(t, reg.Register(c))
	require.NoError(t, testutil.GatherAndCompare(reg, strings.NewReader(`
# HELP test A test metric from the max sample collector.
# TYPE test gauge
test 3
`), "test"))
}

func TestMaxSampleCollector_Add_Sub(t *testing.T) {
	c := NewMaxSampleCollector("test", "")
	require.Equal(t, int64(0), c.val.Load())
	c.Add(2)
	require.Equal(t, int64(2), c.val.Load())
	c.Sub(1)
	require.Equal(t, int64(1), c.val.Load())
}

func TestMaxSampleCollector_Inc_Dec(t *testing.T) {
	c := NewMaxSampleCollector("test", "")
	require.Equal(t, int64(0), c.val.Load())
	c.Inc()
	require.Equal(t, int64(1), c.val.Load())
	c.Inc()
	require.Equal(t, int64(2), c.val.Load())
	c.Dec()
	require.Equal(t, int64(1), c.val.Load())
}

func ExampleMaxSampleCollector() {
	c := NewMaxSampleCollector("metric_name", "This is the metric description, or help message.")
	reg := prometheus.NewRegistry()
	reg.MustRegister(c)
	// The metric_name metric will be scraped via /metrics.
}
