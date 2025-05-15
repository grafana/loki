package streams

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gogo/protobuf/proto"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	sectionLabels = prometheus.Labels{"section": encoding.SectionTypeStreams.String()}
)

// Metrics instruments the streams section.
type Metrics struct {
	encodeSeconds prometheus.Histogram
	recordsTotal  prometheus.Counter
	streamCount   prometheus.Gauge
	minTimestamp  prometheus.Gauge
	maxTimestamp  prometheus.Gauge

	datasetColumnMetadataSize      prometheus.Histogram
	datasetColumnMetadataTotalSize prometheus.Histogram

	datasetColumnCount             prometheus.Histogram
	datasetColumnCompressedBytes   *prometheus.HistogramVec
	datasetColumnUncompressedBytes *prometheus.HistogramVec
	datasetColumnCompressionRatio  *prometheus.HistogramVec
	datasetColumnRows              *prometheus.HistogramVec
	datasetColumnValues            *prometheus.HistogramVec

	datasetPageCount             *prometheus.HistogramVec
	datasetPageCompressedBytes   *prometheus.HistogramVec
	datasetPageUncompressedBytes *prometheus.HistogramVec
	datasetPageCompressionRatio  *prometheus.HistogramVec
	datasetPageRows              *prometheus.HistogramVec
	datasetPageValues            *prometheus.HistogramVec
}

// NewMetrics creates a new set of metrics for the streams section.
func NewMetrics() *Metrics {
	return &Metrics{
		encodeSeconds: prometheus.NewHistogram(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "streams",
			Name:      "encode_seconds",

			Help: "Time taken encoding streams section in seconds.",

			Buckets:                         prometheus.DefBuckets,
			NativeHistogramBucketFactor:     1.1,
			NativeHistogramMaxBucketNumber:  100,
			NativeHistogramMinResetDuration: time.Hour,
		}),

		recordsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "loki_dataobj",
			Subsystem: "streams",
			Name:      "records_total",

			Help: "The total number of stream records appended.",
		}),

		streamCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "streams",
			Name:      "stream_count",

			Help: "The current number of tracked streams; this resets after an encode.",
		}),

		minTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "streams",
			Name:      "min_timestamp",

			Help: "The minimum timestamp (in unix seconds) across all streams; this resets after an encode.",
		}),

		maxTimestamp: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "streams",
			Name:      "max_timestamp",

			Help: "The maximum timestamp (in unix seconds) across all streams; this resets after an encode.",
		}),

		datasetColumnMetadataSize: newNativeHistogram(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_metadata_size",
			Help:      "Distribution of column metadata size per encoded dataset column.",

			ConstLabels: sectionLabels,
		}),

		datasetColumnMetadataTotalSize: newNativeHistogram(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_metadata_total_size",
			Help:      "Distribution of metadata size across all columns per encoded section.",

			ConstLabels: sectionLabels,
		}),

		datasetColumnCount: newNativeHistogram(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_count",
			Help:      "Distribution of column counts per encoded dataset section.",

			ConstLabels: sectionLabels,
		}),

		datasetColumnCompressedBytes: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_compressed_bytes",
			Help:      "Distribution of compressed bytes per encoded dataset column.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		datasetColumnUncompressedBytes: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_uncompressed_bytes",
			Help:      "Distribution of uncompressed bytes per encoded dataset column.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		datasetColumnCompressionRatio: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_compression_ratio",
			Help:      "Distribution of compression ratio per encoded dataset column. Not reported when compression is disabled.",

			ConstLabels: sectionLabels,
		}, []string{"column_type", "compression_type"}),

		datasetColumnRows: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_rows",
			Help:      "Distribution of row counts per encoded dataset column.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		datasetColumnValues: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_values",
			Help:      "Distribution of value counts per encoded dataset column.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		datasetPageCount: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_page_count",
			Help:      "Distribution of page count per encoded dataset column.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		datasetPageCompressedBytes: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_page_compressed_bytes",
			Help:      "Distribution of compressed bytes per encoded dataset page.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		datasetPageUncompressedBytes: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_page_uncompressed_bytes",
			Help:      "Distribution of uncompressed bytes per encoded dataset page.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		datasetPageCompressionRatio: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_page_compression_ratio",
			Help:      "Distribution of compression ratio per encoded dataset page. Not reported when compression is disabled.",

			ConstLabels: sectionLabels,
		}, []string{"column_type", "compression_type"}),

		datasetPageRows: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_page_rows",
			Help:      "Distribution of row counts per encoded dataset page",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		datasetPageValues: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_page_values",
			Help:      "Distribution of value counts per encoded dataset page",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),
	}
}

// Register registers metrics to report to reg.
func (m *Metrics) Register(reg prometheus.Registerer) error {
	var errs []error
	errs = append(errs, reg.Register(m.encodeSeconds))
	errs = append(errs, reg.Register(m.recordsTotal))
	errs = append(errs, reg.Register(m.streamCount))
	errs = append(errs, reg.Register(m.minTimestamp))
	errs = append(errs, reg.Register(m.maxTimestamp))
	errs = append(errs, reg.Register(m.datasetColumnMetadataSize))
	errs = append(errs, reg.Register(m.datasetColumnMetadataTotalSize))
	errs = append(errs, reg.Register(m.datasetColumnCount))
	errs = append(errs, reg.Register(m.datasetColumnCompressedBytes))
	errs = append(errs, reg.Register(m.datasetColumnUncompressedBytes))
	errs = append(errs, reg.Register(m.datasetColumnCompressionRatio))
	errs = append(errs, reg.Register(m.datasetColumnRows))
	errs = append(errs, reg.Register(m.datasetColumnValues))
	errs = append(errs, reg.Register(m.datasetPageCount))
	errs = append(errs, reg.Register(m.datasetPageCompressedBytes))
	errs = append(errs, reg.Register(m.datasetPageUncompressedBytes))
	errs = append(errs, reg.Register(m.datasetPageCompressionRatio))
	errs = append(errs, reg.Register(m.datasetPageRows))
	errs = append(errs, reg.Register(m.datasetPageValues))
	return errors.Join(errs...)
}

// Unregister unregisters metrics from the provided Registerer.
func (m *Metrics) Unregister(reg prometheus.Registerer) {
	reg.Unregister(m.encodeSeconds)
	reg.Unregister(m.recordsTotal)
	reg.Unregister(m.streamCount)
	reg.Unregister(m.minTimestamp)
	reg.Unregister(m.maxTimestamp)
	reg.Unregister(m.datasetColumnMetadataSize)
	reg.Unregister(m.datasetColumnMetadataTotalSize)
	reg.Unregister(m.datasetColumnCount)
	reg.Unregister(m.datasetColumnCompressedBytes)
	reg.Unregister(m.datasetColumnUncompressedBytes)
	reg.Unregister(m.datasetColumnCompressionRatio)
	reg.Unregister(m.datasetColumnRows)
	reg.Unregister(m.datasetColumnValues)
	reg.Unregister(m.datasetPageCount)
	reg.Unregister(m.datasetPageCompressedBytes)
	reg.Unregister(m.datasetPageUncompressedBytes)
	reg.Unregister(m.datasetPageCompressionRatio)
	reg.Unregister(m.datasetPageRows)
	reg.Unregister(m.datasetPageValues)
}

// Observe observes section statistics for a given section's [Decoder].
func (m *Metrics) Observe(ctx context.Context, dec *Decoder) error {
	columns, err := dec.Columns(ctx)
	if err != nil {
		return err
	}
	m.datasetColumnCount.Observe(float64(len(columns)))

	columnPages, err := result.Collect(dec.Pages(ctx, columns))
	if err != nil {
		return err
	} else if len(columnPages) != len(columns) {
		return fmt.Errorf("expected %d page lists, got %d", len(columns), len(columnPages))
	}

	// Count metadata sizes across columns.
	{
		var totalColumnMetadataSize int
		for i := range columns {
			columnMetadataSize := proto.Size(&streamsmd.ColumnMetadata{Pages: columnPages[i]})
			m.datasetColumnMetadataSize.Observe(float64(columnMetadataSize))
			totalColumnMetadataSize += columnMetadataSize
		}
		m.datasetColumnMetadataTotalSize.Observe(float64(totalColumnMetadataSize))
	}

	for i, column := range columns {
		columnType := column.Type.String()
		pages := columnPages[i]
		compression := column.Info.Compression

		m.datasetColumnCompressedBytes.WithLabelValues(columnType).Observe(float64(column.Info.CompressedSize))
		m.datasetColumnUncompressedBytes.WithLabelValues(columnType).Observe(float64(column.Info.UncompressedSize))
		if compression != datasetmd.COMPRESSION_TYPE_NONE {
			m.datasetColumnCompressionRatio.WithLabelValues(columnType, compression.String()).Observe(float64(column.Info.UncompressedSize) / float64(column.Info.CompressedSize))
		}
		m.datasetColumnRows.WithLabelValues(columnType).Observe(float64(column.Info.RowsCount))
		m.datasetColumnValues.WithLabelValues(columnType).Observe(float64(column.Info.ValuesCount))

		m.datasetPageCount.WithLabelValues(columnType).Observe(float64(len(pages)))

		for _, page := range pages {
			m.datasetPageCompressedBytes.WithLabelValues(columnType).Observe(float64(page.Info.CompressedSize))
			m.datasetPageUncompressedBytes.WithLabelValues(columnType).Observe(float64(page.Info.UncompressedSize))
			if compression != datasetmd.COMPRESSION_TYPE_NONE {
				m.datasetPageCompressionRatio.WithLabelValues(columnType, compression.String()).Observe(float64(page.Info.UncompressedSize) / float64(page.Info.CompressedSize))
			}
			m.datasetPageRows.WithLabelValues(columnType).Observe(float64(page.Info.RowsCount))
			m.datasetPageValues.WithLabelValues(columnType).Observe(float64(page.Info.ValuesCount))
		}
	}

	return nil
}

func newNativeHistogram(opts prometheus.HistogramOpts) prometheus.Histogram {
	opts.NativeHistogramBucketFactor = 1.1
	opts.NativeHistogramMaxBucketNumber = 100
	opts.NativeHistogramMinResetDuration = time.Hour

	return prometheus.NewHistogram(opts)
}

func newNativeHistogramVec(opts prometheus.HistogramOpts, labels []string) *prometheus.HistogramVec {
	opts.NativeHistogramBucketFactor = 1.1
	opts.NativeHistogramMaxBucketNumber = 100
	opts.NativeHistogramMinResetDuration = time.Hour

	return prometheus.NewHistogramVec(opts, labels)
}
