package columnar

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gogo/protobuf/proto"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// Metrics instruments the a columnar section. Metrics are only updated when
// calling [Metrics.Observe].
type Metrics struct {
	columnMetadataSize      prometheus.Histogram
	columnMetadataTotalSize prometheus.Histogram

	columnCount             prometheus.Histogram
	columnCompressedBytes   *prometheus.HistogramVec
	columnUncompressedBytes *prometheus.HistogramVec
	columnCompressionRatio  *prometheus.HistogramVec
	columnRows              *prometheus.HistogramVec
	columnValues            *prometheus.HistogramVec

	pageCount             *prometheus.HistogramVec
	pageCompressedBytes   *prometheus.HistogramVec
	pageUncompressedBytes *prometheus.HistogramVec
	pageCompressionRatio  *prometheus.HistogramVec
	pageRows              *prometheus.HistogramVec
	pageValues            *prometheus.HistogramVec
}

// NewMetrics creates a new set of metrics for the streams section. Call
// [Metrics.Observe] to update the metrics for a given section.
func NewMetrics(sectionType dataobj.SectionType) *Metrics {
	// Create the name without the version specified.
	sectionName := sectionType.Namespace + "/" + sectionType.Kind
	sectionLabels := prometheus.Labels{"section": sectionName}

	return &Metrics{
		columnMetadataSize: newNativeHistogram(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_column_metadata_size",
			Help: "Distribution of column metadata size per encoded dataset column.",

			ConstLabels: sectionLabels,
		}),

		columnMetadataTotalSize: newNativeHistogram(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_column_metadata_total_size",
			Help: "Distribution of metadata size across all columns per encoded section.",

			ConstLabels: sectionLabels,
		}),

		columnCount: newNativeHistogram(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_column_count",
			Help: "Distribution of column counts per encoded dataset section.",

			ConstLabels: sectionLabels,
		}),

		columnCompressedBytes: newNativeHistogramVec(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_column_compressed_bytes",
			Help: "Distribution of compressed bytes per encoded dataset column.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		columnUncompressedBytes: newNativeHistogramVec(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_column_uncompressed_bytes",
			Help: "Distribution of uncompressed bytes per encoded dataset column.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		columnCompressionRatio: newNativeHistogramVec(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_column_compression_ratio",
			Help: "Distribution of compression ratio per encoded dataset column. Not reported when compression is disabled.",

			ConstLabels: sectionLabels,
		}, []string{"column_type", "compression_type"}),

		columnRows: newNativeHistogramVec(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_column_rows",
			Help: "Distribution of row counts per encoded dataset column.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		columnValues: newNativeHistogramVec(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_column_values",
			Help: "Distribution of value counts per encoded dataset column.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		pageCount: newNativeHistogramVec(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_page_count",
			Help: "Distribution of page count per encoded dataset column.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		pageCompressedBytes: newNativeHistogramVec(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_page_compressed_bytes",
			Help: "Distribution of compressed bytes per encoded dataset page.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		pageUncompressedBytes: newNativeHistogramVec(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_page_uncompressed_bytes",
			Help: "Distribution of uncompressed bytes per encoded dataset page.",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		pageCompressionRatio: newNativeHistogramVec(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_page_compression_ratio",
			Help: "Distribution of compression ratio per encoded dataset page. Not reported when compression is disabled.",

			ConstLabels: sectionLabels,
		}, []string{"column_type", "compression_type"}),

		pageRows: newNativeHistogramVec(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_page_rows",
			Help: "Distribution of row counts per encoded dataset page",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),

		pageValues: newNativeHistogramVec(prometheus.HistogramOpts{
			Name: "loki_dataobj_encoding_dataset_page_values",
			Help: "Distribution of value counts per encoded dataset page",

			ConstLabels: sectionLabels,
		}, []string{"column_type"}),
	}
}

// Register registers metrics to report to reg.
func (m *Metrics) Register(reg prometheus.Registerer) error {
	var errs []error
	errs = append(errs, reg.Register(m.columnMetadataSize))
	errs = append(errs, reg.Register(m.columnMetadataTotalSize))
	errs = append(errs, reg.Register(m.columnCount))
	errs = append(errs, reg.Register(m.columnCompressedBytes))
	errs = append(errs, reg.Register(m.columnUncompressedBytes))
	errs = append(errs, reg.Register(m.columnCompressionRatio))
	errs = append(errs, reg.Register(m.columnRows))
	errs = append(errs, reg.Register(m.columnValues))
	errs = append(errs, reg.Register(m.pageCount))
	errs = append(errs, reg.Register(m.pageCompressedBytes))
	errs = append(errs, reg.Register(m.pageUncompressedBytes))
	errs = append(errs, reg.Register(m.pageCompressionRatio))
	errs = append(errs, reg.Register(m.pageRows))
	errs = append(errs, reg.Register(m.pageValues))
	return errors.Join(errs...)
}

// Unregister unregisters metrics from the provided Registerer.
func (m *Metrics) Unregister(reg prometheus.Registerer) {
	reg.Unregister(m.columnMetadataSize)
	reg.Unregister(m.columnMetadataTotalSize)
	reg.Unregister(m.columnCount)
	reg.Unregister(m.columnCompressedBytes)
	reg.Unregister(m.columnUncompressedBytes)
	reg.Unregister(m.columnCompressionRatio)
	reg.Unregister(m.columnRows)
	reg.Unregister(m.columnValues)
	reg.Unregister(m.pageCount)
	reg.Unregister(m.pageCompressedBytes)
	reg.Unregister(m.pageUncompressedBytes)
	reg.Unregister(m.pageCompressionRatio)
	reg.Unregister(m.pageRows)
	reg.Unregister(m.pageValues)
}

// Observe observes section statistics for a given section.
func (m *Metrics) Observe(ctx context.Context, section *Section) error {
	dec := section.Decoder()
	metadata, err := dec.SectionMetadata(ctx)
	if err != nil {
		return err
	}
	columnDescs := metadata.GetColumns()
	m.columnCount.Observe(float64(len(columnDescs)))

	columns := section.Columns()

	columnPages, err := result.Collect(dec.Pages(ctx, columnDescs))
	if err != nil {
		return err
	} else if len(columnPages) != len(columnDescs) {
		return fmt.Errorf("expected %d page lists, got %d", len(columnDescs), len(columnPages))
	}

	// Count metadata sizes across columns.
	{
		var totalColumnMetadataSize int
		for i := range columns {
			columnMetadataSize := proto.Size(&datasetmd.ColumnMetadata{Pages: columnPages[i]})
			m.columnMetadataSize.Observe(float64(columnMetadataSize))
			totalColumnMetadataSize += columnMetadataSize
		}
		m.columnMetadataTotalSize.Observe(float64(totalColumnMetadataSize))
	}

	for i, column := range columns {
		columnType := column.Type.Logical + "(" + column.Type.Physical.String() + ")"
		pages := columnPages[i]
		compression := column.desc.Compression

		m.columnCompressedBytes.WithLabelValues(columnType).Observe(float64(column.desc.CompressedSize))
		m.columnUncompressedBytes.WithLabelValues(columnType).Observe(float64(column.desc.UncompressedSize))
		if compression != datasetmd.COMPRESSION_TYPE_NONE {
			m.columnCompressionRatio.WithLabelValues(columnType, compression.String()).Observe(float64(column.desc.UncompressedSize) / float64(column.desc.CompressedSize))
		}
		m.columnRows.WithLabelValues(columnType).Observe(float64(column.desc.RowsCount))
		m.columnValues.WithLabelValues(columnType).Observe(float64(column.desc.ValuesCount))

		m.pageCount.WithLabelValues(columnType).Observe(float64(len(pages)))

		for _, page := range pages {
			m.pageCompressedBytes.WithLabelValues(columnType).Observe(float64(page.CompressedSize))
			m.pageUncompressedBytes.WithLabelValues(columnType).Observe(float64(page.UncompressedSize))
			if compression != datasetmd.COMPRESSION_TYPE_NONE {
				m.pageCompressionRatio.WithLabelValues(columnType, compression.String()).Observe(float64(page.UncompressedSize) / float64(page.CompressedSize))
			}
			m.pageRows.WithLabelValues(columnType).Observe(float64(page.RowsCount))
			m.pageValues.WithLabelValues(columnType).Observe(float64(page.ValuesCount))
		}
	}

	return nil
}

func physicalTypeFriendlyName(ty datasetmd.PhysicalType) string {
	switch ty {
	case datasetmd.PHYSICAL_TYPE_UNSPECIFIED:
		return "invalid"
	case datasetmd.PHYSICAL_TYPE_INT64:
		return "int64"
	case datasetmd.PHYSICAL_TYPE_UINT64:
		return "uint64"
	case datasetmd.PHYSICAL_TYPE_BINARY:
		return "binary"
	}

	return "<unknown>"
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
