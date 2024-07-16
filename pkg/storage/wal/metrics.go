package wal

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type ManagerMetrics struct {
	NumAvailable prometheus.Gauge
	NumPending   prometheus.Gauge
	NumFlushing  prometheus.Gauge
}

func NewManagerMetrics(r prometheus.Registerer) *ManagerMetrics {
	return &ManagerMetrics{
		NumAvailable: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "wal_segments_available",
			Help: "The number of WAL segments accepting writes.",
		}),
		NumPending: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "wal_segments_pending",
			Help: "The number of WAL segments waiting to be flushed.",
		}),
		NumFlushing: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "wal_segments_flushing",
			Help: "The number of WAL segments being flushed.",
		}),
	}
}

type SegmentMetrics struct {
	outputSizeBytes prometheus.Histogram
	inputSizeBytes  prometheus.Histogram
	streams         prometheus.Histogram
	tenants         prometheus.Histogram
}

func NewSegmentMetrics(r prometheus.Registerer) *SegmentMetrics {
	return &SegmentMetrics{
		outputSizeBytes: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_segment_output_size_bytes",
			Help:                        "The segment size as written to disk (compressed).",
			Buckets:                     prometheus.ExponentialBuckets(100, 10, 8),
			NativeHistogramBucketFactor: 1.1,
		}),
		inputSizeBytes: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_segment_input_size_bytes",
			Help:                        "The segment size (uncompressed).",
			Buckets:                     prometheus.ExponentialBuckets(100, 10, 8),
			NativeHistogramBucketFactor: 1.1,
		}),
		streams: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_per_segment_streams",
			Help:                        "The number of streams per segment.",
			Buckets:                     prometheus.ExponentialBuckets(1, 2, 10),
			NativeHistogramBucketFactor: 1.1,
		}),
		tenants: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_per_segment_tenants",
			Help:                        "The number of tenants per segment.",
			Buckets:                     prometheus.ExponentialBuckets(1, 2, 10),
			NativeHistogramBucketFactor: 1.1,
		}),
	}
}

type Metrics struct {
	SegmentMetrics *SegmentMetrics
	ManagerMetrics *ManagerMetrics
}

func NewMetrics(r prometheus.Registerer) *Metrics {
	return &Metrics{
		ManagerMetrics: NewManagerMetrics(r),
		SegmentMetrics: NewSegmentMetrics(r),
	}
}
