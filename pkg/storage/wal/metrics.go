package wal

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

type ManagerMetrics struct {
	NumAvailable prometheus.Gauge
	NumFlushing  prometheus.Gauge
	NumPending   prometheus.Gauge
}

func NewManagerMetrics(r prometheus.Registerer) *ManagerMetrics {
	return &ManagerMetrics{
		NumAvailable: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "wal_segments_available",
			Help: "The number of WAL segments accepting writes.",
		}),
		NumFlushing: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "wal_segments_flushing",
			Help: "The number of WAL segments being flushed.",
		}),
		NumPending: promauto.With(r).NewGauge(prometheus.GaugeOpts{
			Name: "wal_segments_pending",
			Help: "The number of WAL segments waiting to be flushed.",
		}),
	}
}

type SegmentMetrics struct {
	age       prometheus.Histogram
	size      prometheus.Histogram
	streams   prometheus.Histogram
	tenants   prometheus.Histogram
	writeSize prometheus.Histogram
}

func NewSegmentMetrics(r prometheus.Registerer) *SegmentMetrics {
	return &SegmentMetrics{
		age: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_segment_age_seconds",
			Help:                        "The segment age (time between first append and flush).",
			Buckets:                     prometheus.ExponentialBuckets(0.001, 4, 8),
			NativeHistogramBucketFactor: 1.1,
		}),
		size: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_segment_size_bytes",
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
		writeSize: promauto.With(r).NewHistogram(prometheus.HistogramOpts{
			Name:                        "loki_ingester_rf1_segment_write_size_bytes",
			Help:                        "The segment size as written to disk (compressed).",
			Buckets:                     prometheus.ExponentialBuckets(100, 10, 8),
			NativeHistogramBucketFactor: 1.1,
		}),
	}
}
