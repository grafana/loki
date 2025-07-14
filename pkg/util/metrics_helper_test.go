package util

import (
	"bytes"
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"testing"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/testutil"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"
)

func TestSum(t *testing.T) {
	require.Equal(t, float64(0), sum(nil, counterValue))
	require.Equal(t, float64(0), sum(&dto.MetricFamily{Metric: nil}, counterValue))
	require.Equal(t, float64(0), sum(&dto.MetricFamily{Metric: []*dto.Metric{{Counter: &dto.Counter{}}}}, counterValue))
	require.Equal(t, 12345.6789, sum(&dto.MetricFamily{Metric: []*dto.Metric{{Counter: &dto.Counter{Value: proto.Float64(12345.6789)}}}}, counterValue))
	require.Equal(t, 20235.80235, sum(&dto.MetricFamily{Metric: []*dto.Metric{
		{Counter: &dto.Counter{Value: proto.Float64(12345.6789)}},
		{Counter: &dto.Counter{Value: proto.Float64(7890.12345)}},
	}}, counterValue))
	// using 'counterValue' as function only sums counters
	require.Equal(t, float64(0), sum(&dto.MetricFamily{Metric: []*dto.Metric{
		{Gauge: &dto.Gauge{Value: proto.Float64(12345.6789)}},
		{Gauge: &dto.Gauge{Value: proto.Float64(7890.12345)}},
	}}, counterValue))
}

func Test_maxMetric(t *testing.T) {
	require.Equal(t, float64(0), maxMetric(nil, counterValue))
	require.Equal(t, float64(0), maxMetric(&dto.MetricFamily{Metric: nil}, counterValue))
	require.Equal(t, float64(0), maxMetric(&dto.MetricFamily{Metric: []*dto.Metric{{Counter: &dto.Counter{}}}}, counterValue))
	require.Equal(t, 12345.6789, maxMetric(&dto.MetricFamily{Metric: []*dto.Metric{{Counter: &dto.Counter{Value: proto.Float64(12345.6789)}}}}, counterValue))
	require.Equal(t, 7890.12345, maxMetric(&dto.MetricFamily{Metric: []*dto.Metric{
		{Counter: &dto.Counter{Value: proto.Float64(1234.56789)}},
		{Counter: &dto.Counter{Value: proto.Float64(7890.12345)}},
	}}, counterValue))
	// using 'counterValue' as function only works on counters
	require.Equal(t, float64(0), maxMetric(&dto.MetricFamily{Metric: []*dto.Metric{
		{Gauge: &dto.Gauge{Value: proto.Float64(12345.6789)}},
		{Gauge: &dto.Gauge{Value: proto.Float64(7890.12345)}},
	}}, counterValue))
}

func TestCounterValue(t *testing.T) {
	require.Equal(t, float64(0), counterValue(nil))
	require.Equal(t, float64(0), counterValue(&dto.Metric{}))
	require.Equal(t, float64(0), counterValue(&dto.Metric{Counter: &dto.Counter{}}))
	require.Equal(t, float64(543857.12837), counterValue(&dto.Metric{Counter: &dto.Counter{Value: proto.Float64(543857.12837)}}))
}

func TestGetMetricsWithLabelNames(t *testing.T) {
	labels := []string{"a", "b"}

	require.Equal(t, map[string]metricsWithLabels{}, getMetricsWithLabelNames(nil, labels, nil))
	require.Equal(t, map[string]metricsWithLabels{}, getMetricsWithLabelNames(&dto.MetricFamily{}, labels, nil))

	m1 := &dto.Metric{Label: makeLabels("a", "5"), Counter: &dto.Counter{Value: proto.Float64(1)}}
	m2 := &dto.Metric{Label: makeLabels("a", "10", "b", "20"), Counter: &dto.Counter{Value: proto.Float64(1.5)}}
	m3 := &dto.Metric{Label: makeLabels("a", "10", "b", "20", "c", "1"), Counter: &dto.Counter{Value: proto.Float64(2)}}
	m4 := &dto.Metric{Label: makeLabels("a", "10", "b", "20", "c", "2"), Counter: &dto.Counter{Value: proto.Float64(3)}}
	m5 := &dto.Metric{Label: makeLabels("a", "11", "b", "21"), Counter: &dto.Counter{Value: proto.Float64(4)}}
	m6 := &dto.Metric{Label: makeLabels("ignored", "123", "a", "12", "b", "22", "c", "30"), Counter: &dto.Counter{Value: proto.Float64(4)}}

	out := getMetricsWithLabelNames(&dto.MetricFamily{Metric: []*dto.Metric{m1, m2, m3, m4, m5, m6}}, labels, nil)

	// m1 is not returned at all, as it doesn't have both required labels.
	require.Equal(t, map[string]metricsWithLabels{
		getLabelsString([]string{"10", "20"}): {
			labelValues: []string{"10", "20"},
			metrics:     []*dto.Metric{m2, m3, m4}},
		getLabelsString([]string{"11", "21"}): {
			labelValues: []string{"11", "21"},
			metrics:     []*dto.Metric{m5}},
		getLabelsString([]string{"12", "22"}): {
			labelValues: []string{"12", "22"},
			metrics:     []*dto.Metric{m6}},
	}, out)

	// no labels -- returns all metrics in single key. this isn't very efficient, and there are other functions
	// (without labels) to handle this better, but it still works.
	out2 := getMetricsWithLabelNames(&dto.MetricFamily{Metric: []*dto.Metric{m1, m2, m3, m4, m5, m6}}, []string{}, nil)
	require.Equal(t, map[string]metricsWithLabels{
		getLabelsString(nil): {
			labelValues: []string{},
			metrics:     []*dto.Metric{m1, m2, m3, m4, m5, m6}},
	}, out2)
}

func BenchmarkGetMetricsWithLabelNames(b *testing.B) {
	const (
		numMetrics         = 1000
		numLabelsPerMetric = 10
	)

	// Generate metrics and add them to a metric family.
	mf := &dto.MetricFamily{Metric: make([]*dto.Metric, 0, numMetrics)}
	for i := 0; i < numMetrics; i++ {
		labels := []*dto.LabelPair{{
			Name:  proto.String("unique"),
			Value: proto.String(strconv.Itoa(i)),
		}}

		for l := 1; l < numLabelsPerMetric; l++ {
			labels = append(labels, &dto.LabelPair{
				Name:  proto.String(fmt.Sprintf("label_%d", l)),
				Value: proto.String(fmt.Sprintf("value_%d", l)),
			})
		}

		mf.Metric = append(mf.Metric, &dto.Metric{
			Label:   labels,
			Counter: &dto.Counter{Value: proto.Float64(1.5)},
		})
	}

	b.ResetTimer()
	b.ReportAllocs()

	for n := 0; n < b.N; n++ {
		out := getMetricsWithLabelNames(mf, []string{"label_1", "label_2", "label_3"}, nil)

		if expected := 1; len(out) != expected {
			b.Fatalf("unexpected number of output groups: expected = %d got = %d", expected, len(out))
		}
	}
}

func makeLabels(namesAndValues ...string) []*dto.LabelPair {
	out := []*dto.LabelPair(nil)

	for i := 0; i+1 < len(namesAndValues); i = i + 2 {
		out = append(out, &dto.LabelPair{
			Name:  proto.String(namesAndValues[i]),
			Value: proto.String(namesAndValues[i+1]),
		})
	}

	return out
}

// TestSendSumOfGaugesPerUserWithLabels tests to ensure multiple metrics for the same user with a matching label are
// summed correctly
func TestSendSumOfGaugesPerUserWithLabels(t *testing.T) {
	user1Metric := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "test_metric"}, []string{"label_one", "label_two"})
	user2Metric := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "test_metric"}, []string{"label_one", "label_two"})
	user1Metric.WithLabelValues("a", "b").Set(100)
	user1Metric.WithLabelValues("a", "c").Set(80)
	user2Metric.WithLabelValues("a", "b").Set(60)
	user2Metric.WithLabelValues("a", "c").Set(40)

	user1Reg := prometheus.NewRegistry()
	user2Reg := prometheus.NewRegistry()
	user1Reg.MustRegister(user1Metric)
	user2Reg.MustRegister(user2Metric)

	regs := NewUserRegistries()
	regs.AddUserRegistry("user-1", user1Reg)
	regs.AddUserRegistry("user-2", user2Reg)
	mf := regs.BuildMetricFamiliesPerUser(nil)

	{
		desc := prometheus.NewDesc("test_metric", "", []string{"user", "label_one"}, nil)
		actual := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfGaugesPerUserWithLabels(out, desc, "test_metric", "label_one")
		})
		expected := []*dto.Metric{
			{Label: makeLabels("label_one", "a", "user", "user-1"), Gauge: &dto.Gauge{Value: proto.Float64(180)}},
			{Label: makeLabels("label_one", "a", "user", "user-2"), Gauge: &dto.Gauge{Value: proto.Float64(100)}},
		}
		require.ElementsMatch(t, expected, actual)
	}

	{
		desc := prometheus.NewDesc("test_metric", "", []string{"user", "label_two"}, nil)
		actual := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfGaugesPerUserWithLabels(out, desc, "test_metric", "label_two")
		})
		expected := []*dto.Metric{
			{Label: makeLabels("label_two", "b", "user", "user-1"), Gauge: &dto.Gauge{Value: proto.Float64(100)}},
			{Label: makeLabels("label_two", "c", "user", "user-1"), Gauge: &dto.Gauge{Value: proto.Float64(80)}},
			{Label: makeLabels("label_two", "b", "user", "user-2"), Gauge: &dto.Gauge{Value: proto.Float64(60)}},
			{Label: makeLabels("label_two", "c", "user", "user-2"), Gauge: &dto.Gauge{Value: proto.Float64(40)}},
		}
		require.ElementsMatch(t, expected, actual)
	}

	{
		desc := prometheus.NewDesc("test_metric", "", []string{"user", "label_one", "label_two"}, nil)
		actual := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfGaugesPerUserWithLabels(out, desc, "test_metric", "label_one", "label_two")
		})
		expected := []*dto.Metric{
			{Label: makeLabels("label_one", "a", "label_two", "b", "user", "user-1"), Gauge: &dto.Gauge{Value: proto.Float64(100)}},
			{Label: makeLabels("label_one", "a", "label_two", "c", "user", "user-1"), Gauge: &dto.Gauge{Value: proto.Float64(80)}},
			{Label: makeLabels("label_one", "a", "label_two", "b", "user", "user-2"), Gauge: &dto.Gauge{Value: proto.Float64(60)}},
			{Label: makeLabels("label_one", "a", "label_two", "c", "user", "user-2"), Gauge: &dto.Gauge{Value: proto.Float64(40)}},
		}
		require.ElementsMatch(t, expected, actual)
	}
}

func TestSendMaxOfGauges(t *testing.T) {
	user1Reg := prometheus.NewRegistry()
	user2Reg := prometheus.NewRegistry()
	desc := prometheus.NewDesc("test_metric", "", nil, nil)
	regs := NewUserRegistries()
	regs.AddUserRegistry("user-1", user1Reg)
	regs.AddUserRegistry("user-2", user2Reg)

	// No matching metric.
	mf := regs.BuildMetricFamiliesPerUser(nil)
	actual := collectMetrics(t, func(out chan prometheus.Metric) {
		mf.SendMaxOfGauges(out, desc, "test_metric")
	})
	expected := []*dto.Metric{
		{Label: nil, Gauge: &dto.Gauge{Value: proto.Float64(0)}},
	}
	require.ElementsMatch(t, expected, actual)

	// Register a metric for each user.
	user1Metric := promauto.With(user1Reg).NewGauge(prometheus.GaugeOpts{Name: "test_metric"})
	user2Metric := promauto.With(user2Reg).NewGauge(prometheus.GaugeOpts{Name: "test_metric"})
	user1Metric.Set(100)
	user2Metric.Set(80)
	mf = regs.BuildMetricFamiliesPerUser(nil)

	actual = collectMetrics(t, func(out chan prometheus.Metric) {
		mf.SendMaxOfGauges(out, desc, "test_metric")
	})
	expected = []*dto.Metric{
		{Label: nil, Gauge: &dto.Gauge{Value: proto.Float64(100)}},
	}
	require.ElementsMatch(t, expected, actual)
}

func TestSendSumOfHistogramsWithLabels(t *testing.T) {
	buckets := []float64{1, 2, 3}
	user1Metric := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_metric", Buckets: buckets}, []string{"label_one", "label_two"})
	user2Metric := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "test_metric", Buckets: buckets}, []string{"label_one", "label_two"})
	user1Metric.WithLabelValues("a", "b").Observe(1)
	user1Metric.WithLabelValues("a", "c").Observe(2)
	user2Metric.WithLabelValues("a", "b").Observe(3)
	user2Metric.WithLabelValues("a", "c").Observe(4)

	user1Reg := prometheus.NewRegistry()
	user2Reg := prometheus.NewRegistry()
	user1Reg.MustRegister(user1Metric)
	user2Reg.MustRegister(user2Metric)

	regs := NewUserRegistries()
	regs.AddUserRegistry("user-1", user1Reg)
	regs.AddUserRegistry("user-2", user2Reg)
	mf := regs.BuildMetricFamiliesPerUser(nil)

	{
		desc := prometheus.NewDesc("test_metric", "", []string{"label_one"}, nil)
		actual := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfHistogramsWithLabels(out, desc, "test_metric", "label_one")
		})
		expected := []*dto.Metric{
			{Label: makeLabels("label_one", "a"), Histogram: &dto.Histogram{SampleCount: uint64p(4), SampleSum: float64p(10), Bucket: []*dto.Bucket{
				{UpperBound: float64p(1), CumulativeCount: uint64p(1)},
				{UpperBound: float64p(2), CumulativeCount: uint64p(2)},
				{UpperBound: float64p(3), CumulativeCount: uint64p(3)},
			}}},
		}
		require.ElementsMatch(t, expected, actual)
	}

	{
		desc := prometheus.NewDesc("test_metric", "", []string{"label_two"}, nil)
		actual := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfHistogramsWithLabels(out, desc, "test_metric", "label_two")
		})
		expected := []*dto.Metric{
			{Label: makeLabels("label_two", "b"), Histogram: &dto.Histogram{SampleCount: uint64p(2), SampleSum: float64p(4), Bucket: []*dto.Bucket{
				{UpperBound: float64p(1), CumulativeCount: uint64p(1)},
				{UpperBound: float64p(2), CumulativeCount: uint64p(1)},
				{UpperBound: float64p(3), CumulativeCount: uint64p(2)},
			}}},
			{Label: makeLabels("label_two", "c"), Histogram: &dto.Histogram{SampleCount: uint64p(2), SampleSum: float64p(6), Bucket: []*dto.Bucket{
				{UpperBound: float64p(1), CumulativeCount: uint64p(0)},
				{UpperBound: float64p(2), CumulativeCount: uint64p(1)},
				{UpperBound: float64p(3), CumulativeCount: uint64p(1)},
			}}},
		}
		require.ElementsMatch(t, expected, actual)
	}

	{
		desc := prometheus.NewDesc("test_metric", "", []string{"label_one", "label_two"}, nil)
		actual := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfHistogramsWithLabels(out, desc, "test_metric", "label_one", "label_two")
		})
		expected := []*dto.Metric{
			{Label: makeLabels("label_one", "a", "label_two", "b"), Histogram: &dto.Histogram{SampleCount: uint64p(2), SampleSum: float64p(4), Bucket: []*dto.Bucket{
				{UpperBound: float64p(1), CumulativeCount: uint64p(1)},
				{UpperBound: float64p(2), CumulativeCount: uint64p(1)},
				{UpperBound: float64p(3), CumulativeCount: uint64p(2)},
			}}},
			{Label: makeLabels("label_one", "a", "label_two", "c"), Histogram: &dto.Histogram{SampleCount: uint64p(2), SampleSum: float64p(6), Bucket: []*dto.Bucket{
				{UpperBound: float64p(1), CumulativeCount: uint64p(0)},
				{UpperBound: float64p(2), CumulativeCount: uint64p(1)},
				{UpperBound: float64p(3), CumulativeCount: uint64p(1)},
			}}},
		}
		require.ElementsMatch(t, expected, actual)
	}
}

// TestSumOfCounterPerUserWithLabels tests to ensure multiple metrics for the same user with a matching label are
// summed correctly
func TestSumOfCounterPerUserWithLabels(t *testing.T) {
	user1Metric := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_metric"}, []string{"label_one", "label_two"})
	user2Metric := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "test_metric"}, []string{"label_one", "label_two"})
	user1Metric.WithLabelValues("a", "b").Add(100)
	user1Metric.WithLabelValues("a", "c").Add(80)
	user2Metric.WithLabelValues("a", "b").Add(60)
	user2Metric.WithLabelValues("a", "c").Add(40)

	user1Reg := prometheus.NewRegistry()
	user2Reg := prometheus.NewRegistry()
	user1Reg.MustRegister(user1Metric)
	user2Reg.MustRegister(user2Metric)

	regs := NewUserRegistries()
	regs.AddUserRegistry("user-1", user1Reg)
	regs.AddUserRegistry("user-2", user2Reg)
	mf := regs.BuildMetricFamiliesPerUser(nil)

	{
		desc := prometheus.NewDesc("test_metric", "", []string{"user", "label_one"}, nil)
		actual := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfCountersPerUserWithLabels(out, desc, "test_metric", "label_one")
		})
		expected := []*dto.Metric{
			{Label: makeLabels("label_one", "a", "user", "user-1"), Counter: &dto.Counter{Value: proto.Float64(180)}},
			{Label: makeLabels("label_one", "a", "user", "user-2"), Counter: &dto.Counter{Value: proto.Float64(100)}},
		}
		require.ElementsMatch(t, expected, actual)
	}

	{
		desc := prometheus.NewDesc("test_metric", "", []string{"user", "label_two"}, nil)
		actual := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfCountersPerUserWithLabels(out, desc, "test_metric", "label_two")
		})
		expected := []*dto.Metric{
			{Label: makeLabels("label_two", "b", "user", "user-1"), Counter: &dto.Counter{Value: proto.Float64(100)}},
			{Label: makeLabels("label_two", "c", "user", "user-1"), Counter: &dto.Counter{Value: proto.Float64(80)}},
			{Label: makeLabels("label_two", "b", "user", "user-2"), Counter: &dto.Counter{Value: proto.Float64(60)}},
			{Label: makeLabels("label_two", "c", "user", "user-2"), Counter: &dto.Counter{Value: proto.Float64(40)}},
		}
		require.ElementsMatch(t, expected, actual)
	}

	{
		desc := prometheus.NewDesc("test_metric", "", []string{"user", "label_one", "label_two"}, nil)
		actual := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfCountersPerUserWithLabels(out, desc, "test_metric", "label_one", "label_two")
		})
		expected := []*dto.Metric{
			{Label: makeLabels("label_one", "a", "label_two", "b", "user", "user-1"), Counter: &dto.Counter{Value: proto.Float64(100)}},
			{Label: makeLabels("label_one", "a", "label_two", "c", "user", "user-1"), Counter: &dto.Counter{Value: proto.Float64(80)}},
			{Label: makeLabels("label_one", "a", "label_two", "b", "user", "user-2"), Counter: &dto.Counter{Value: proto.Float64(60)}},
			{Label: makeLabels("label_one", "a", "label_two", "c", "user", "user-2"), Counter: &dto.Counter{Value: proto.Float64(40)}},
		}
		require.ElementsMatch(t, expected, actual)
	}
}

func TestSendSumOfSummariesPerUser(t *testing.T) {
	objectives := map[float64]float64{0.25: 25, 0.5: 50, 0.75: 75}
	user1Metric := prometheus.NewSummary(prometheus.SummaryOpts{Name: "test_metric", Objectives: objectives})
	user2Metric := prometheus.NewSummary(prometheus.SummaryOpts{Name: "test_metric", Objectives: objectives})
	user1Metric.Observe(25)
	user1Metric.Observe(50)
	user1Metric.Observe(75)
	user2Metric.Observe(25)
	user2Metric.Observe(50)
	user2Metric.Observe(76)

	user1Reg := prometheus.NewRegistry()
	user2Reg := prometheus.NewRegistry()
	user1Reg.MustRegister(user1Metric)
	user2Reg.MustRegister(user2Metric)

	regs := NewUserRegistries()
	regs.AddUserRegistry("user-1", user1Reg)
	regs.AddUserRegistry("user-2", user2Reg)
	mf := regs.BuildMetricFamiliesPerUser(nil)

	{
		desc := prometheus.NewDesc("test_metric", "", []string{"user"}, nil)
		actual := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfSummariesPerUser(out, desc, "test_metric")
		})
		expected := []*dto.Metric{
			{
				Label: makeLabels("user", "user-1"),
				Summary: &dto.Summary{
					SampleCount: uint64p(3),
					SampleSum:   float64p(150),
					Quantile: []*dto.Quantile{
						{
							Quantile: proto.Float64(.25),
							Value:    proto.Float64(25),
						},
						{
							Quantile: proto.Float64(.5),
							Value:    proto.Float64(50),
						},
						{
							Quantile: proto.Float64(.75),
							Value:    proto.Float64(75),
						},
					},
				},
			},
			{
				Label: makeLabels("user", "user-2"),
				Summary: &dto.Summary{
					SampleCount: uint64p(3),
					SampleSum:   float64p(151),
					Quantile: []*dto.Quantile{
						{
							Quantile: proto.Float64(.25),
							Value:    proto.Float64(25),
						},
						{
							Quantile: proto.Float64(.5),
							Value:    proto.Float64(50),
						},
						{
							Quantile: proto.Float64(.75),
							Value:    proto.Float64(76),
						},
					},
				},
			},
		}
		require.ElementsMatch(t, expected, actual)
	}
}

func TestFloat64PrecisionStability(t *testing.T) {
	const (
		numRuns       = 100
		numRegistries = 100
		cardinality   = 20
	)

	// Randomise the seed but log it in case we need to reproduce the test on failure.
	seed := time.Now().UnixNano()
	randomGenerator := rand.New(rand.NewSource(seed))
	t.Log("random generator seed:", seed)

	// Generate a large number of registries with different metrics each.
	registries := NewUserRegistries()
	for userID := 1; userID <= numRegistries; userID++ {
		reg := prometheus.NewRegistry()
		labelNames := []string{"label_one", "label_two"}

		g := promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{Name: "test_gauge"}, labelNames)
		for i := 0; i < cardinality; i++ {
			g.WithLabelValues("a", strconv.Itoa(i)).Set(randomGenerator.Float64())
		}

		c := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{Name: "test_counter"}, labelNames)
		for i := 0; i < cardinality; i++ {
			c.WithLabelValues("a", strconv.Itoa(i)).Add(randomGenerator.Float64())
		}

		h := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{Name: "test_histogram", Buckets: []float64{0.1, 0.5, 1}}, labelNames)
		for i := 0; i < cardinality; i++ {
			h.WithLabelValues("a", strconv.Itoa(i)).Observe(randomGenerator.Float64())
		}

		s := promauto.With(reg).NewSummaryVec(prometheus.SummaryOpts{Name: "test_summary"}, labelNames)
		for i := 0; i < cardinality; i++ {
			s.WithLabelValues("a", strconv.Itoa(i)).Observe(randomGenerator.Float64())
		}

		registries.AddUserRegistry(strconv.Itoa(userID), reg)
	}

	// Ensure multiple runs always return the same exact results.
	expected := map[string][]*dto.Metric{}

	for run := 0; run < numRuns; run++ {
		mf := registries.BuildMetricFamiliesPerUser(nil)

		gauge := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfGauges(out, prometheus.NewDesc("test_gauge", "", nil, nil), "test_gauge")
		})
		gaugeWithLabels := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfGaugesWithLabels(out, prometheus.NewDesc("test_gauge", "", []string{"label_one"}, nil), "test_gauge", "label_one")
		})

		counter := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfCounters(out, prometheus.NewDesc("test_counter", "", nil, nil), "test_counter")
		})
		counterWithLabels := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfCountersWithLabels(out, prometheus.NewDesc("test_counter", "", []string{"label_one"}, nil), "test_counter", "label_one")
		})

		histogram := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfHistograms(out, prometheus.NewDesc("test_histogram", "", nil, nil), "test_histogram")
		})
		histogramWithLabels := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfHistogramsWithLabels(out, prometheus.NewDesc("test_histogram", "", []string{"label_one"}, nil), "test_histogram", "label_one")
		})

		summary := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfSummaries(out, prometheus.NewDesc("test_summary", "", nil, nil), "test_summary")
		})
		summaryWithLabels := collectMetrics(t, func(out chan prometheus.Metric) {
			mf.SendSumOfSummariesWithLabels(out, prometheus.NewDesc("test_summary", "", []string{"label_one"}, nil), "test_summary", "label_one")
		})

		// The first run we just store the expected value.
		if run == 0 {
			expected["gauge"] = gauge
			expected["gauge_with_labels"] = gaugeWithLabels
			expected["counter"] = counter
			expected["counter_with_labels"] = counterWithLabels
			expected["histogram"] = histogram
			expected["histogram_with_labels"] = histogramWithLabels
			expected["summary"] = summary
			expected["summary_with_labels"] = summaryWithLabels
			continue
		}

		// All subsequent runs we assert the actual metric with the expected one.
		require.Equal(t, expected["gauge"], gauge)
		require.Equal(t, expected["gauge_with_labels"], gaugeWithLabels)
		require.Equal(t, expected["counter"], counter)
		require.Equal(t, expected["counter_with_labels"], counterWithLabels)
		require.Equal(t, expected["histogram"], histogram)
		require.Equal(t, expected["histogram_with_labels"], histogramWithLabels)
		require.Equal(t, expected["summary"], summary)
		require.Equal(t, expected["summary_with_labels"], summaryWithLabels)
	}
}

// This test is a baseline for following tests, that remove or replace a registry.
// It shows output for metrics from setupTestMetrics before doing any modifications.
func TestUserRegistries_RemoveBaseline(t *testing.T) {
	mainRegistry := prometheus.NewPedanticRegistry()
	mainRegistry.MustRegister(setupTestMetrics())

	require.NoError(t, testutil.GatherAndCompare(mainRegistry, bytes.NewBufferString(`
		# HELP counter help
		# TYPE counter counter
		counter 75

		# HELP counter_labels help
		# TYPE counter_labels counter
		counter_labels{label_one="a"} 75

		# HELP counter_user help
		# TYPE counter_user counter
		counter_user{user="1"} 5
		counter_user{user="2"} 10
		counter_user{user="3"} 15
		counter_user{user="4"} 20
		counter_user{user="5"} 25

		# HELP gauge help
		# TYPE gauge gauge
		gauge 75

		# HELP gauge_labels help
		# TYPE gauge_labels gauge
		gauge_labels{label_one="a"} 75

		# HELP gauge_user help
		# TYPE gauge_user gauge
		gauge_user{user="1"} 5
		gauge_user{user="2"} 10
		gauge_user{user="3"} 15
		gauge_user{user="4"} 20
		gauge_user{user="5"} 25

		# HELP histogram help
		# TYPE histogram histogram
		histogram_bucket{le="1"} 5
		histogram_bucket{le="3"} 15
		histogram_bucket{le="5"} 25
		histogram_bucket{le="+Inf"} 25
		histogram_sum 75
		histogram_count 25

		# HELP histogram_labels help
		# TYPE histogram_labels histogram
		histogram_labels_bucket{label_one="a",le="1"} 5
		histogram_labels_bucket{label_one="a",le="3"} 15
		histogram_labels_bucket{label_one="a",le="5"} 25
		histogram_labels_bucket{label_one="a",le="+Inf"} 25
		histogram_labels_sum{label_one="a"} 75
		histogram_labels_count{label_one="a"} 25

		# HELP summary help
		# TYPE summary summary
		summary_sum 75
		summary_count 25

		# HELP summary_labels help
		# TYPE summary_labels summary
		summary_labels_sum{label_one="a"} 75
		summary_labels_count{label_one="a"} 25

		# HELP summary_user help
		# TYPE summary_user summary
		summary_user_sum{user="1"} 5
		summary_user_count{user="1"} 5
		summary_user_sum{user="2"} 10
		summary_user_count{user="2"} 5
		summary_user_sum{user="3"} 15
		summary_user_count{user="3"} 5
		summary_user_sum{user="4"} 20
		summary_user_count{user="4"} 5
		summary_user_sum{user="5"} 25
		summary_user_count{user="5"} 5
	`)))
}

func TestUserRegistries_RemoveUserRegistry_SoftRemoval(t *testing.T) {
	tm := setupTestMetrics()

	mainRegistry := prometheus.NewPedanticRegistry()
	mainRegistry.MustRegister(tm)

	// Soft-remove single registry.
	tm.regs.RemoveUserRegistry(strconv.Itoa(3), false)

	require.NoError(t, testutil.GatherAndCompare(mainRegistry, bytes.NewBufferString(`
			# HELP counter help
			# TYPE counter counter
	# No change in counter
			counter 75
	
			# HELP counter_labels help
			# TYPE counter_labels counter
	# No change in counter per label.
			counter_labels{label_one="a"} 75
	
			# HELP counter_user help
			# TYPE counter_user counter
	# User 3 is now missing.
			counter_user{user="1"} 5
			counter_user{user="2"} 10
			counter_user{user="4"} 20
			counter_user{user="5"} 25
	
			# HELP gauge help
			# TYPE gauge gauge
	# Drop in the gauge (value 3, counted 5 times)
			gauge 60
	
			# HELP gauge_labels help
			# TYPE gauge_labels gauge
	# Drop in the gauge (value 3, counted 5 times)
			gauge_labels{label_one="a"} 60
	
			# HELP gauge_user help
			# TYPE gauge_user gauge
	# User 3 is now missing.
			gauge_user{user="1"} 5
			gauge_user{user="2"} 10
			gauge_user{user="4"} 20
			gauge_user{user="5"} 25
	
			# HELP histogram help
			# TYPE histogram histogram
	# No change in the histogram
			histogram_bucket{le="1"} 5
			histogram_bucket{le="3"} 15
			histogram_bucket{le="5"} 25
			histogram_bucket{le="+Inf"} 25
			histogram_sum 75
			histogram_count 25
	
			# HELP histogram_labels help
			# TYPE histogram_labels histogram
	# No change in the histogram per label
			histogram_labels_bucket{label_one="a",le="1"} 5
			histogram_labels_bucket{label_one="a",le="3"} 15
			histogram_labels_bucket{label_one="a",le="5"} 25
			histogram_labels_bucket{label_one="a",le="+Inf"} 25
			histogram_labels_sum{label_one="a"} 75
			histogram_labels_count{label_one="a"} 25
	
			# HELP summary help
			# TYPE summary summary
	# No change in the summary
			summary_sum 75
			summary_count 25
	
			# HELP summary_labels help
			# TYPE summary_labels summary
	# No change in the summary per label
			summary_labels_sum{label_one="a"} 75
			summary_labels_count{label_one="a"} 25
	
			# HELP summary_user help
			# TYPE summary_user summary
	# Summary for user 3 is now missing.
			summary_user_sum{user="1"} 5
			summary_user_count{user="1"} 5
			summary_user_sum{user="2"} 10
			summary_user_count{user="2"} 5
			summary_user_sum{user="4"} 20
			summary_user_count{user="4"} 5
			summary_user_sum{user="5"} 25
			summary_user_count{user="5"} 5
	`)))
}
func TestUserRegistries_RemoveUserRegistry_HardRemoval(t *testing.T) {
	tm := setupTestMetrics()

	mainRegistry := prometheus.NewPedanticRegistry()
	mainRegistry.MustRegister(tm)

	// Soft-remove single registry.
	tm.regs.RemoveUserRegistry(strconv.Itoa(3), true)

	require.NoError(t, testutil.GatherAndCompare(mainRegistry, bytes.NewBufferString(`
			# HELP counter help
			# TYPE counter counter
	# Counter drop (reset!)
			counter 60
	
			# HELP counter_labels help
			# TYPE counter_labels counter
	# Counter drop (reset!)
			counter_labels{label_one="a"} 60
	
			# HELP counter_user help
			# TYPE counter_user counter
	# User 3 is now missing.
			counter_user{user="1"} 5
			counter_user{user="2"} 10
			counter_user{user="4"} 20
			counter_user{user="5"} 25
	
			# HELP gauge help
			# TYPE gauge gauge
	# Drop in the gauge (value 3, counted 5 times)
			gauge 60
	
			# HELP gauge_labels help
			# TYPE gauge_labels gauge
	# Drop in the gauge (value 3, counted 5 times)
			gauge_labels{label_one="a"} 60
	
			# HELP gauge_user help
			# TYPE gauge_user gauge
	# User 3 is now missing.
			gauge_user{user="1"} 5
			gauge_user{user="2"} 10
			gauge_user{user="4"} 20
			gauge_user{user="5"} 25
	
			# HELP histogram help
			# TYPE histogram histogram
	# Histogram drop (reset for sum and count!)
			histogram_bucket{le="1"} 5
			histogram_bucket{le="3"} 10
			histogram_bucket{le="5"} 20
			histogram_bucket{le="+Inf"} 20
			histogram_sum 60
			histogram_count 20
	
			# HELP histogram_labels help
			# TYPE histogram_labels histogram
	# No change in the histogram per label
			histogram_labels_bucket{label_one="a",le="1"} 5
			histogram_labels_bucket{label_one="a",le="3"} 10
			histogram_labels_bucket{label_one="a",le="5"} 20
			histogram_labels_bucket{label_one="a",le="+Inf"} 20
			histogram_labels_sum{label_one="a"} 60
			histogram_labels_count{label_one="a"} 20
	
			# HELP summary help
			# TYPE summary summary
	# Summary drop!
			summary_sum 60
			summary_count 20
	
			# HELP summary_labels help
			# TYPE summary_labels summary
	# Summary drop!
			summary_labels_sum{label_one="a"} 60
			summary_labels_count{label_one="a"} 20
	
			# HELP summary_user help
			# TYPE summary_user summary
	# Summary for user 3 is now missing.
			summary_user_sum{user="1"} 5
			summary_user_count{user="1"} 5
			summary_user_sum{user="2"} 10
			summary_user_count{user="2"} 5
			summary_user_sum{user="4"} 20
			summary_user_count{user="4"} 5
			summary_user_sum{user="5"} 25
			summary_user_count{user="5"} 5
	`)))
}

func TestUserRegistries_AddUserRegistry_ReplaceRegistry(t *testing.T) {
	tm := setupTestMetrics()

	mainRegistry := prometheus.NewPedanticRegistry()
	mainRegistry.MustRegister(tm)

	// Replace registry for user 5 with empty registry. Replacement does soft-delete previous registry.
	tm.regs.AddUserRegistry(strconv.Itoa(5), prometheus.NewRegistry())

	require.NoError(t, testutil.GatherAndCompare(mainRegistry, bytes.NewBufferString(`
			# HELP counter help
			# TYPE counter counter
	# No change in counter
			counter 75
	
			# HELP counter_labels help
			# TYPE counter_labels counter
	# No change in counter per label
			counter_labels{label_one="a"} 75
	
			# HELP counter_user help
			# TYPE counter_user counter
	# Per-user counter now missing.
			counter_user{user="1"} 5
			counter_user{user="2"} 10
			counter_user{user="3"} 15
			counter_user{user="4"} 20
	
			# HELP gauge help
			# TYPE gauge gauge
	# Gauge drops by 25 (value for user 5, times 5)
			gauge 50
	
			# HELP gauge_labels help
			# TYPE gauge_labels gauge
	# Gauge drops by 25 (value for user 5, times 5)
			gauge_labels{label_one="a"} 50
	
			# HELP gauge_user help
			# TYPE gauge_user gauge
	# Gauge for user 5 is missing
			gauge_user{user="1"} 5
			gauge_user{user="2"} 10
			gauge_user{user="3"} 15
			gauge_user{user="4"} 20
	
			# HELP histogram help
			# TYPE histogram histogram
	# No change in histogram
			histogram_bucket{le="1"} 5
			histogram_bucket{le="3"} 15
			histogram_bucket{le="5"} 25
			histogram_bucket{le="+Inf"} 25
			histogram_sum 75
			histogram_count 25
	
			# HELP histogram_labels help
			# TYPE histogram_labels histogram
	# No change in histogram per label.
			histogram_labels_bucket{label_one="a",le="1"} 5
			histogram_labels_bucket{label_one="a",le="3"} 15
			histogram_labels_bucket{label_one="a",le="5"} 25
			histogram_labels_bucket{label_one="a",le="+Inf"} 25
			histogram_labels_sum{label_one="a"} 75
			histogram_labels_count{label_one="a"} 25
	
			# HELP summary help
			# TYPE summary summary
	# No change in summary
			summary_sum 75
			summary_count 25
	
			# HELP summary_labels help
			# TYPE summary_labels summary
	# No change in summary per label
			summary_labels_sum{label_one="a"} 75
			summary_labels_count{label_one="a"} 25
	
			# HELP summary_user help
			# TYPE summary_user summary
	# Summary for user 5 now zero (reset)
			summary_user_sum{user="1"} 5
			summary_user_count{user="1"} 5
			summary_user_sum{user="2"} 10
			summary_user_count{user="2"} 5
			summary_user_sum{user="3"} 15
			summary_user_count{user="3"} 5
			summary_user_sum{user="4"} 20
			summary_user_count{user="4"} 5
			summary_user_sum{user="5"} 0
			summary_user_count{user="5"} 0
	`)))
}

func setupTestMetrics() *testMetrics {
	const numUsers = 5
	const cardinality = 5

	tm := newTestMetrics()

	for userID := 1; userID <= numUsers; userID++ {
		reg := prometheus.NewRegistry()

		labelNames := []string{"label_one", "label_two"}

		g := promauto.With(reg).NewGaugeVec(prometheus.GaugeOpts{Name: "test_gauge"}, labelNames)
		for i := 0; i < cardinality; i++ {
			g.WithLabelValues("a", strconv.Itoa(i)).Set(float64(userID))
		}

		c := promauto.With(reg).NewCounterVec(prometheus.CounterOpts{Name: "test_counter"}, labelNames)
		for i := 0; i < cardinality; i++ {
			c.WithLabelValues("a", strconv.Itoa(i)).Add(float64(userID))
		}

		h := promauto.With(reg).NewHistogramVec(prometheus.HistogramOpts{Name: "test_histogram", Buckets: []float64{1, 3, 5}}, labelNames)
		for i := 0; i < cardinality; i++ {
			h.WithLabelValues("a", strconv.Itoa(i)).Observe(float64(userID))
		}

		s := promauto.With(reg).NewSummaryVec(prometheus.SummaryOpts{Name: "test_summary"}, labelNames)
		for i := 0; i < cardinality; i++ {
			s.WithLabelValues("a", strconv.Itoa(i)).Observe(float64(userID))
		}

		tm.regs.AddUserRegistry(strconv.Itoa(userID), reg)
	}
	return tm
}

type testMetrics struct {
	regs *UserRegistries

	gauge             *prometheus.Desc
	gaugePerUser      *prometheus.Desc
	gaugeWithLabels   *prometheus.Desc
	counter           *prometheus.Desc
	counterPerUser    *prometheus.Desc
	counterWithLabels *prometheus.Desc
	histogram         *prometheus.Desc
	histogramLabels   *prometheus.Desc
	summary           *prometheus.Desc
	summaryPerUser    *prometheus.Desc
	summaryLabels     *prometheus.Desc
}

func newTestMetrics() *testMetrics {
	return &testMetrics{
		regs: NewUserRegistries(),

		gauge:             prometheus.NewDesc("gauge", "help", nil, nil),
		gaugePerUser:      prometheus.NewDesc("gauge_user", "help", []string{"user"}, nil),
		gaugeWithLabels:   prometheus.NewDesc("gauge_labels", "help", []string{"label_one"}, nil),
		counter:           prometheus.NewDesc("counter", "help", nil, nil),
		counterPerUser:    prometheus.NewDesc("counter_user", "help", []string{"user"}, nil),
		counterWithLabels: prometheus.NewDesc("counter_labels", "help", []string{"label_one"}, nil),
		histogram:         prometheus.NewDesc("histogram", "help", nil, nil),
		histogramLabels:   prometheus.NewDesc("histogram_labels", "help", []string{"label_one"}, nil),
		summary:           prometheus.NewDesc("summary", "help", nil, nil),
		summaryPerUser:    prometheus.NewDesc("summary_user", "help", []string{"user"}, nil),
		summaryLabels:     prometheus.NewDesc("summary_labels", "help", []string{"label_one"}, nil),
	}
}

func (tm *testMetrics) Describe(out chan<- *prometheus.Desc) {
	out <- tm.gauge
	out <- tm.gaugePerUser
	out <- tm.gaugeWithLabels
	out <- tm.counter
	out <- tm.counterPerUser
	out <- tm.counterWithLabels
	out <- tm.histogram
	out <- tm.histogramLabels
	out <- tm.summary
	out <- tm.summaryPerUser
	out <- tm.summaryLabels
}

func (tm *testMetrics) Collect(out chan<- prometheus.Metric) {
	data := tm.regs.BuildMetricFamiliesPerUser(nil)

	data.SendSumOfGauges(out, tm.gauge, "test_gauge")
	data.SendSumOfGaugesPerUser(out, tm.gaugePerUser, "test_gauge")
	data.SendSumOfGaugesWithLabels(out, tm.gaugeWithLabels, "test_gauge", "label_one")
	data.SendSumOfCounters(out, tm.counter, "test_counter")
	data.SendSumOfCountersPerUser(out, tm.counterPerUser, "test_counter")
	data.SendSumOfCountersWithLabels(out, tm.counterWithLabels, "test_counter", "label_one")
	data.SendSumOfHistograms(out, tm.histogram, "test_histogram")
	data.SendSumOfHistogramsWithLabels(out, tm.histogramLabels, "test_histogram", "label_one")
	data.SendSumOfSummaries(out, tm.summary, "test_summary")
	data.SendSumOfSummariesPerUser(out, tm.summaryPerUser, "test_summary")
	data.SendSumOfSummariesWithLabels(out, tm.summaryLabels, "test_summary", "label_one")
}

func collectMetrics(t *testing.T, send func(out chan prometheus.Metric)) []*dto.Metric {
	out := make(chan prometheus.Metric)

	go func() {
		send(out)
		close(out)
	}()

	var metrics []*dto.Metric
	for m := range out {
		collected := &dto.Metric{}
		err := m.Write(collected)
		require.NoError(t, err)

		metrics = append(metrics, collected)
	}

	return metrics
}

func float64p(v float64) *float64 {
	return &v
}

func uint64p(v uint64) *uint64 {
	return &v
}

func BenchmarkGetLabels_SmallSet(b *testing.B) {
	m := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test",
		ConstLabels: map[string]string{
			"cluster": "abc",
		},
	}, []string{"reason", "user"})

	m.WithLabelValues("bad", "user1").Inc()
	m.WithLabelValues("worse", "user1").Inc()
	m.WithLabelValues("worst", "user1").Inc()

	m.WithLabelValues("bad", "user2").Inc()
	m.WithLabelValues("worst", "user2").Inc()

	m.WithLabelValues("worst", "user3").Inc()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := GetLabels(m, map[string]string{"user": "user1", "reason": "worse"}); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkGetLabels_MediumSet(b *testing.B) {
	m := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test",
		ConstLabels: map[string]string{
			"cluster": "abc",
		},
	}, []string{"reason", "user"})

	for i := 1; i <= 1000; i++ {
		m.WithLabelValues("bad", fmt.Sprintf("user%d", i)).Inc()
		m.WithLabelValues("worse", fmt.Sprintf("user%d", i)).Inc()
		m.WithLabelValues("worst", fmt.Sprintf("user%d", i)).Inc()

		if i%2 == 0 {
			m.WithLabelValues("bad", fmt.Sprintf("user%d", i)).Inc()
			m.WithLabelValues("worst", fmt.Sprintf("user%d", i)).Inc()
		} else {
			m.WithLabelValues("worst", fmt.Sprintf("user%d", i)).Inc()
		}
	}
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		if _, err := GetLabels(m, map[string]string{"user": "user1", "reason": "worse"}); err != nil {
			b.Fatal(err)
		}
	}
}

func TestGetLabels(t *testing.T) {
	m := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "test",
		ConstLabels: map[string]string{
			"cluster": "abc",
		},
	}, []string{"reason", "user"})

	m.WithLabelValues("bad", "user1").Inc()
	m.WithLabelValues("worse", "user1").Inc()
	m.WithLabelValues("worst", "user1").Inc()

	m.WithLabelValues("bad", "user2").Inc()
	m.WithLabelValues("worst", "user2").Inc()

	m.WithLabelValues("worst", "user3").Inc()

	verifyLabels(t, m, nil, []labels.Labels{
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "bad", "user": "user1"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "worse", "user": "user1"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "worst", "user": "user1"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "bad", "user": "user2"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "worst", "user": "user2"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "worst", "user": "user3"}),
	})

	verifyLabels(t, m, map[string]string{"cluster": "abc"}, []labels.Labels{
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "bad", "user": "user1"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "worse", "user": "user1"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "worst", "user": "user1"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "bad", "user": "user2"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "worst", "user": "user2"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "worst", "user": "user3"}),
	})

	verifyLabels(t, m, map[string]string{"reason": "bad"}, []labels.Labels{
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "bad", "user": "user1"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "bad", "user": "user2"}),
	})

	verifyLabels(t, m, map[string]string{"user": "user1"}, []labels.Labels{
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "bad", "user": "user1"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "worse", "user": "user1"}),
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "worst", "user": "user1"}),
	})

	verifyLabels(t, m, map[string]string{"user": "user1", "reason": "worse"}, []labels.Labels{
		labels.FromMap(map[string]string{"cluster": "abc", "reason": "worse", "user": "user1"}),
	})
}

func verifyLabels(t *testing.T, m prometheus.Collector, filter map[string]string, expectedLabels []labels.Labels) {
	result, err := GetLabels(m, filter)
	require.NoError(t, err)

	sort.Slice(result, func(i, j int) bool {
		return labels.Compare(result[i], result[j]) < 0
	})

	sort.Slice(expectedLabels, func(i, j int) bool {
		return labels.Compare(expectedLabels[i], expectedLabels[j]) < 0
	})

	require.Equal(t, expectedLabels, result)
}

func TestHumanizeBytes(t *testing.T) {
	tests := map[uint64]string{
		1024:               "1.0kB",
		1024 * 1000:        "1.0MB",
		1024 * 1000 * 1000: "1.0GB",
		10:                 "10B",
	}

	for bytes, humanizedBytes := range tests {
		t.Run(fmt.Sprintf("%d", bytes), func(t *testing.T) {
			require.Equal(t, humanizedBytes, HumanizeBytes(bytes))
		})
	}
}
