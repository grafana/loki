package util

import (
	"bytes"
	"errors"
	"fmt"

	"github.com/go-kit/kit/log/level"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// MetricFamiliesPerUser is a collection of metrics gathered via calling Gatherer.Gather() method on different
// gatherers, one per user.
// First key = userID, second key = metric name.
// Value = slice of gathered values with the same metric name.
type MetricFamiliesPerUser map[string]map[string]*dto.MetricFamily

func BuildMetricFamiliesPerUserFromUserRegistries(regs map[string]*prometheus.Registry) MetricFamiliesPerUser {
	data := MetricFamiliesPerUser{}
	for userID, r := range regs {
		m, err := r.Gather()
		if err == nil {
			err = data.AddGatheredDataForUser(userID, m)
		}

		if err != nil {
			level.Warn(Logger).Log("msg", "failed to gather metrics from registry", "user", userID, "err", err)
			continue
		}
	}
	return data
}

// AddGatheredDataForUser adds user-specific output of Gatherer.Gather method.
// Gatherer.Gather specifies that there metric families are uniquely named, and we use that fact here.
// If they are not, this method returns error.
func (d MetricFamiliesPerUser) AddGatheredDataForUser(userID string, metrics []*dto.MetricFamily) error {
	// Keeping map of metric name to its family makes it easier to do searches later.
	perMetricName := map[string]*dto.MetricFamily{}

	for _, m := range metrics {
		name := m.GetName()
		// these errors should never happen when passing Gatherer.Gather() output.
		if name == "" {
			return errors.New("empty name for metric family")
		}
		if perMetricName[name] != nil {
			return fmt.Errorf("non-unique name for metric family: %q", name)
		}

		perMetricName[name] = m
	}

	d[userID] = perMetricName
	return nil
}

func (d MetricFamiliesPerUser) SendSumOfCounters(out chan<- prometheus.Metric, desc *prometheus.Desc, counter string) {
	result := float64(0)
	for _, perMetric := range d {
		result += sum(perMetric[counter], counterValue)
	}

	out <- prometheus.MustNewConstMetric(desc, prometheus.CounterValue, result)
}

func (d MetricFamiliesPerUser) SendSumOfCountersWithLabels(out chan<- prometheus.Metric, desc *prometheus.Desc, counter string, labelNames ...string) {
	result := d.sumOfSingleValuesWithLabels(counter, counterValue, labelNames)
	for _, cr := range result {
		out <- prometheus.MustNewConstMetric(desc, prometheus.CounterValue, cr.value, cr.labelValues...)
	}
}

func (d MetricFamiliesPerUser) SendSumOfCountersPerUser(out chan<- prometheus.Metric, desc *prometheus.Desc, counter string) {
	for user, perMetric := range d {
		v := sum(perMetric[counter], counterValue)

		out <- prometheus.MustNewConstMetric(desc, prometheus.CounterValue, v, user)
	}
}

func (d MetricFamiliesPerUser) SendSumOfGauges(out chan<- prometheus.Metric, desc *prometheus.Desc, gauge string) {
	result := float64(0)
	for _, perMetric := range d {
		result += sum(perMetric[gauge], gaugeValue)
	}
	out <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, result)
}

func (d MetricFamiliesPerUser) SendSumOfGaugesWithLabels(out chan<- prometheus.Metric, desc *prometheus.Desc, gauge string, labelNames ...string) {
	result := d.sumOfSingleValuesWithLabels(gauge, gaugeValue, labelNames)
	for _, cr := range result {
		out <- prometheus.MustNewConstMetric(desc, prometheus.GaugeValue, cr.value, cr.labelValues...)
	}
}

type singleResult struct {
	value       float64
	labelValues []string
}

func (d MetricFamiliesPerUser) sumOfSingleValuesWithLabels(metric string, fn func(*dto.Metric) float64, labelNames []string) map[string]singleResult {
	result := map[string]singleResult{}

	for _, userMetrics := range d {
		metricsPerLabelValue := getMetricsWithLabelNames(userMetrics[metric], labelNames)

		for key, mlv := range metricsPerLabelValue {
			for _, m := range mlv.metrics {
				r := result[key]
				if r.labelValues == nil {
					r.labelValues = mlv.labelValues
				}

				r.value += fn(m)
				result[key] = r
			}
		}
	}

	return result
}

func (d MetricFamiliesPerUser) SendSumOfSummaries(out chan<- prometheus.Metric, desc *prometheus.Desc, summaryName string) {
	var (
		sampleCount uint64
		sampleSum   float64
		quantiles   map[float64]float64
	)

	for _, userMetrics := range d {
		for _, m := range userMetrics[summaryName].GetMetric() {
			summary := m.GetSummary()
			sampleCount += summary.GetSampleCount()
			sampleSum += summary.GetSampleSum()
			quantiles = mergeSummaryQuantiles(quantiles, summary.GetQuantile())
		}
	}

	out <- prometheus.MustNewConstSummary(desc, sampleCount, sampleSum, quantiles)
}

func (d MetricFamiliesPerUser) SendSumOfSummariesWithLabels(out chan<- prometheus.Metric, desc *prometheus.Desc, summaryName string, labelNames ...string) {
	type summaryResult struct {
		sampleCount uint64
		sampleSum   float64
		quantiles   map[float64]float64
		labelValues []string
	}

	result := map[string]summaryResult{}

	for _, userMetrics := range d {
		metricsPerLabelValue := getMetricsWithLabelNames(userMetrics[summaryName], labelNames)

		for key, mwl := range metricsPerLabelValue {
			for _, m := range mwl.metrics {
				r := result[key]
				if r.labelValues == nil {
					r.labelValues = mwl.labelValues
				}

				summary := m.GetSummary()
				r.sampleCount += summary.GetSampleCount()
				r.sampleSum += summary.GetSampleSum()
				r.quantiles = mergeSummaryQuantiles(r.quantiles, summary.GetQuantile())

				result[key] = r
			}
		}
	}

	for _, sr := range result {
		out <- prometheus.MustNewConstSummary(desc, sr.sampleCount, sr.sampleSum, sr.quantiles, sr.labelValues...)
	}
}

func (d MetricFamiliesPerUser) SendSumOfHistograms(out chan<- prometheus.Metric, desc *prometheus.Desc, histogramName string) {
	var (
		sampleCount uint64
		sampleSum   float64
		buckets     map[float64]uint64
	)

	for _, userMetrics := range d {
		for _, m := range userMetrics[histogramName].GetMetric() {
			histo := m.GetHistogram()
			sampleCount += histo.GetSampleCount()
			sampleSum += histo.GetSampleSum()
			buckets = mergeHistogramBuckets(buckets, histo.GetBucket())
		}
	}

	out <- prometheus.MustNewConstHistogram(desc, sampleCount, sampleSum, buckets)
}

func mergeSummaryQuantiles(quantiles map[float64]float64, summaryQuantiles []*dto.Quantile) map[float64]float64 {
	if len(summaryQuantiles) == 0 {
		return quantiles
	}

	out := quantiles
	if out == nil {
		out = map[float64]float64{}
	}

	for _, q := range summaryQuantiles {
		// we assume that all summaries have same quantiles
		out[q.GetQuantile()] += q.GetValue()
	}
	return out
}

func mergeHistogramBuckets(buckets map[float64]uint64, histogramBuckets []*dto.Bucket) map[float64]uint64 {
	if len(histogramBuckets) == 0 {
		return buckets
	}

	out := buckets
	if out == nil {
		out = map[float64]uint64{}
	}

	for _, q := range histogramBuckets {
		// we assume that all histograms have same buckets
		out[q.GetUpperBound()] += q.GetCumulativeCount()
	}
	return out
}

type metricsWithLabels struct {
	labelValues []string
	metrics     []*dto.Metric
}

func getMetricsWithLabelNames(mf *dto.MetricFamily, labelNames []string) map[string]metricsWithLabels {
	result := map[string]metricsWithLabels{}

	for _, m := range mf.GetMetric() {
		lbls, include := getLabelValues(m, labelNames)
		if !include {
			continue
		}

		key := getLabelsString(lbls)
		r := result[key]
		if r.labelValues == nil {
			r.labelValues = lbls
		}
		r.metrics = append(r.metrics, m)
		result[key] = r
	}
	return result
}

func getLabelValues(m *dto.Metric, labelNames []string) ([]string, bool) {
	all := map[string]string{}
	for _, lp := range m.GetLabel() {
		all[lp.GetName()] = lp.GetValue()
	}

	result := make([]string, 0, len(labelNames))
	for _, ln := range labelNames {
		lv, ok := all[ln]
		if !ok {
			// required labels not found
			return nil, false
		}
		result = append(result, lv)
	}
	return result, true
}

func getLabelsString(labelValues []string) string {
	buf := bytes.Buffer{}
	for _, v := range labelValues {
		buf.WriteString(v)
		buf.WriteByte(0) // separator, not used in prometheus labels
	}
	return buf.String()
}

// sum returns sum of values from all metrics from same metric family (= series with the same metric name, but different labels)
// Supplied function extracts value.
func sum(mf *dto.MetricFamily, fn func(*dto.Metric) float64) float64 {
	result := float64(0)
	for _, m := range mf.GetMetric() {
		result += fn(m)
	}
	return result
}

// This works even if m is nil, m.Counter is nil or m.Counter.Value is nil (it returns 0 in those cases)
func counterValue(m *dto.Metric) float64 { return m.GetCounter().GetValue() }
func gaugeValue(m *dto.Metric) float64   { return m.GetGauge().GetValue() }
