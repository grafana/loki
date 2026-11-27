// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package internal // import "go.opentelemetry.io/collector/pdata/internal"

// SanitizeUTF8ExportMetricsServiceRequest replaces invalid UTF-8 byte sequences in every string field of req
// with the Unicode replacement character U+FFFD.
func SanitizeUTF8ExportMetricsServiceRequest(req *ExportMetricsServiceRequest) {
	for _, rm := range req.ResourceMetrics {
		SanitizeUTF8Resource(&rm.Resource)
		rm.SchemaUrl = SanitizeUTF8String(rm.SchemaUrl)
		sanitizeUTF8ScopeMetricsSlice(rm.ScopeMetrics)
		// DeprecatedScopeMetrics is normally emptied by otlp.MigrateMetrics, but JSON unmarshalling can
		// still populate it, so it is walked as well.
		sanitizeUTF8ScopeMetricsSlice(rm.DeprecatedScopeMetrics)
	}
}

func sanitizeUTF8ScopeMetricsSlice(sms []*ScopeMetrics) {
	for _, sm := range sms {
		SanitizeUTF8InstrumentationScope(&sm.Scope)
		sm.SchemaUrl = SanitizeUTF8String(sm.SchemaUrl)
		for _, m := range sm.Metrics {
			sanitizeUTF8Metric(m)
		}
	}
}

func sanitizeUTF8Metric(m *Metric) {
	m.Name = SanitizeUTF8String(m.Name)
	m.Description = SanitizeUTF8String(m.Description)
	m.Unit = SanitizeUTF8String(m.Unit)
	SanitizeUTF8KeyValueSlice(m.Metadata)
	switch data := m.Data.(type) {
	case *Metric_Gauge:
		if data.Gauge != nil {
			sanitizeUTF8NumberDataPoints(data.Gauge.DataPoints)
		}
	case *Metric_Sum:
		if data.Sum != nil {
			sanitizeUTF8NumberDataPoints(data.Sum.DataPoints)
		}
	case *Metric_Histogram:
		if data.Histogram != nil {
			for _, dp := range data.Histogram.DataPoints {
				SanitizeUTF8KeyValueSlice(dp.Attributes)
				sanitizeUTF8Exemplars(dp.Exemplars)
			}
		}
	case *Metric_ExponentialHistogram:
		if data.ExponentialHistogram != nil {
			for _, dp := range data.ExponentialHistogram.DataPoints {
				SanitizeUTF8KeyValueSlice(dp.Attributes)
				sanitizeUTF8Exemplars(dp.Exemplars)
			}
		}
	case *Metric_Summary:
		if data.Summary != nil {
			for _, dp := range data.Summary.DataPoints {
				SanitizeUTF8KeyValueSlice(dp.Attributes)
			}
		}
	}
}

func sanitizeUTF8NumberDataPoints(dps []*NumberDataPoint) {
	for _, dp := range dps {
		SanitizeUTF8KeyValueSlice(dp.Attributes)
		sanitizeUTF8Exemplars(dp.Exemplars)
	}
}

func sanitizeUTF8Exemplars(exemplars []Exemplar) {
	for i := range exemplars {
		SanitizeUTF8KeyValueSlice(exemplars[i].FilteredAttributes)
	}
}
