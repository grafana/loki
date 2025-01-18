package encoding

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

// Metrics instruments encoded data objects.
type Metrics struct {
	sectionsCount       prometheus.Histogram
	fileMetadataSize    prometheus.Histogram
	sectionMetadataSize *prometheus.HistogramVec

	datasetColumnMetadataSize      *prometheus.HistogramVec
	datasetColumnMetadataTotalSize *prometheus.HistogramVec

	datasetColumnCount             *prometheus.HistogramVec
	datasetColumnCompressedBytes   *prometheus.HistogramVec
	datasetColumnUncompressedBytes *prometheus.HistogramVec
	datasetColumnRows              *prometheus.HistogramVec
	datasetColumnValues            *prometheus.HistogramVec

	datasetPageCount             *prometheus.HistogramVec
	datasetPageCompressedBytes   *prometheus.HistogramVec
	datasetPageUncompressedBytes *prometheus.HistogramVec
	datasetPageRows              *prometheus.HistogramVec
	datasetPageValues            *prometheus.HistogramVec
}

// NewMetrics creates a new set of metrics for encoding.
func NewMetrics() *Metrics {
	// To limit the number of time series per data object builder, these metrics
	// are only available as classic histograms, otherwise we would have 10x the
	// total number of metrics.

	return &Metrics{
		sectionsCount: newNativeHistogram(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "sections_count",
			Help:      "Distribution of sections per encoded data object.",
		}),

		fileMetadataSize: newNativeHistogram(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "file_metadata_size",
			Help:      "Distribution of metadata size per encoded data object.",
		}),

		sectionMetadataSize: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "section_metadata_size",
			Help:      "Distribution of metadata size per encoded section.",
		}, []string{"section"}),

		datasetColumnMetadataSize: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_metadata_size",
			Help:      "Distribution of column metadata size per encoded dataset column.",
		}, []string{"section"}),

		datasetColumnMetadataTotalSize: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_metadata_total_size",
			Help:      "Distribution of metadata size across all columns per encoded section.",
		}, []string{"section"}),

		datasetColumnCount: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_count",
			Help:      "Distribution of column counts per encoded dataset section.",
		}, []string{"section"}),

		datasetColumnCompressedBytes: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_compressed_bytes",
			Help:      "Distribution of compressed bytes per encoded dataset column.",
		}, []string{"section", "column_type"}),

		datasetColumnUncompressedBytes: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_uncompressed_bytes",
			Help:      "Distribution of uncompressed bytes per encoded dataset column.",
		}, []string{"section", "column_type"}),

		datasetColumnRows: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_rows",
			Help:      "Distribution of row counts per encoded dataset column.",
		}, []string{"section", "column_type"}),

		datasetColumnValues: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_column_values",
			Help:      "Distribution of value counts per encoded dataset column.",
		}, []string{"section", "column_type"}),

		datasetPageCount: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_page_count",
			Help:      "Distribution of page count per encoded dataset column.",
		}, []string{"section", "column_type"}),

		datasetPageCompressedBytes: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_page_compressed_bytes",
			Help:      "Distribution of compressed bytes per encoded dataset page.",
		}, []string{"section", "column_type"}),

		datasetPageUncompressedBytes: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_page_uncompressed_bytes",
			Help:      "Distribution of uncompressed bytes per encoded dataset page.",
		}, []string{"section", "column_type"}),

		datasetPageRows: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_page_rows",
			Help:      "Distribution of row counts per encoded dataset page",
		}, []string{"section", "column_type"}),

		datasetPageValues: newNativeHistogramVec(prometheus.HistogramOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "dataset_page_values",
			Help:      "Distribution of value counts per encoded dataset page",
		}, []string{"section", "column_type"}),
	}
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

// Register registers metrics to report to reg.
func (m *Metrics) Register(reg prometheus.Registerer) error {
	var errs []error
	errs = append(errs, reg.Register(m.sectionsCount))
	errs = append(errs, reg.Register(m.fileMetadataSize))
	errs = append(errs, reg.Register(m.sectionMetadataSize))
	errs = append(errs, reg.Register(m.datasetColumnMetadataSize))
	errs = append(errs, reg.Register(m.datasetColumnMetadataTotalSize))
	errs = append(errs, reg.Register(m.datasetColumnCount))
	errs = append(errs, reg.Register(m.datasetColumnCompressedBytes))
	errs = append(errs, reg.Register(m.datasetColumnUncompressedBytes))
	errs = append(errs, reg.Register(m.datasetColumnRows))
	errs = append(errs, reg.Register(m.datasetColumnValues))
	errs = append(errs, reg.Register(m.datasetPageCount))
	errs = append(errs, reg.Register(m.datasetPageCompressedBytes))
	errs = append(errs, reg.Register(m.datasetPageUncompressedBytes))
	errs = append(errs, reg.Register(m.datasetPageRows))
	errs = append(errs, reg.Register(m.datasetPageValues))
	return errors.Join(errs...)
}

// Unregister unregisters metrics from the provided Registerer.
func (m *Metrics) Unregister(reg prometheus.Registerer) {
	reg.Unregister(m.sectionsCount)
	reg.Unregister(m.fileMetadataSize)
	reg.Unregister(m.sectionMetadataSize)
	reg.Unregister(m.datasetColumnMetadataSize)
	reg.Unregister(m.datasetColumnMetadataTotalSize)
	reg.Unregister(m.datasetColumnCount)
	reg.Unregister(m.datasetColumnCompressedBytes)
	reg.Unregister(m.datasetColumnUncompressedBytes)
	reg.Unregister(m.datasetColumnRows)
	reg.Unregister(m.datasetColumnValues)
	reg.Unregister(m.datasetPageCount)
	reg.Unregister(m.datasetPageCompressedBytes)
	reg.Unregister(m.datasetPageUncompressedBytes)
	reg.Unregister(m.datasetPageRows)
	reg.Unregister(m.datasetPageValues)
}

// Observe observes the data object statistics for the given [Decoder].
func (m *Metrics) Observe(ctx context.Context, dec Decoder) error {
	sections, err := dec.Sections(ctx)
	if err != nil {
		return err
	}

	// TODO(rfratto): our Decoder interface should be updated to not hide the
	// metadata types to avoid recreating them here.

	m.sectionsCount.Observe(float64(len(sections)))
	m.fileMetadataSize.Observe(float64(proto.Size(&filemd.Metadata{Sections: sections})))
	for _, section := range sections {
		m.sectionMetadataSize.WithLabelValues(section.Type.String()).Observe(float64(section.MetadataSize))
	}

	var (
		streamsDecoder = dec.StreamsDecoder()
		logsDecoder    = dec.LogsDecoder()
	)

	var errs []error

	for _, section := range sections {
		switch section.Type {
		case filemd.SECTION_TYPE_STREAMS:
			errs = append(errs, m.observeStreamsSection(ctx, section, streamsDecoder))
		case filemd.SECTION_TYPE_LOGS:
			errs = append(errs, m.observeLogsSection(ctx, section, logsDecoder))
		default:
			errs = append(errs, fmt.Errorf("unknown section type %q", section.Type.String()))
		}
	}

	return errors.Join(errs...)
}

func (m *Metrics) observeStreamsSection(ctx context.Context, section *filemd.SectionInfo, dec StreamsDecoder) error {
	sectionType := section.Type.String()

	columns, err := dec.Columns(ctx, section)
	if err != nil {
		return err
	}
	m.datasetColumnCount.WithLabelValues(sectionType).Observe(float64(len(columns)))

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
			m.datasetColumnMetadataSize.WithLabelValues(sectionType).Observe(float64(columnMetadataSize))
			totalColumnMetadataSize += columnMetadataSize
		}
		m.datasetColumnMetadataTotalSize.WithLabelValues(sectionType).Observe(float64(totalColumnMetadataSize))
	}

	for i, column := range columns {
		columnType := column.Type.String()
		pages := columnPages[i]

		m.datasetColumnCompressedBytes.WithLabelValues(sectionType, columnType).Observe(float64(column.Info.CompressedSize))
		m.datasetColumnUncompressedBytes.WithLabelValues(sectionType, columnType).Observe(float64(column.Info.UncompressedSize))
		m.datasetColumnRows.WithLabelValues(sectionType, columnType).Observe(float64(column.Info.RowsCount))
		m.datasetColumnValues.WithLabelValues(sectionType, columnType).Observe(float64(column.Info.ValuesCount))

		m.datasetPageCount.WithLabelValues(sectionType, columnType).Observe(float64(len(pages)))

		for _, page := range pages {
			m.datasetPageCompressedBytes.WithLabelValues(sectionType, columnType).Observe(float64(page.Info.CompressedSize))
			m.datasetPageUncompressedBytes.WithLabelValues(sectionType, columnType).Observe(float64(page.Info.UncompressedSize))
			m.datasetPageRows.WithLabelValues(sectionType, columnType).Observe(float64(page.Info.RowsCount))
			m.datasetPageValues.WithLabelValues(sectionType, columnType).Observe(float64(page.Info.ValuesCount))
		}
	}

	return nil
}

func (m *Metrics) observeLogsSection(ctx context.Context, section *filemd.SectionInfo, dec LogsDecoder) error {
	sectionType := section.Type.String()

	columns, err := dec.Columns(ctx, section)
	if err != nil {
		return err
	}
	m.datasetColumnCount.WithLabelValues(sectionType).Observe(float64(len(columns)))

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
			columnMetadataSize := proto.Size(&logsmd.ColumnMetadata{Pages: columnPages[i]})
			m.datasetColumnMetadataSize.WithLabelValues(sectionType).Observe(float64(columnMetadataSize))
			totalColumnMetadataSize += columnMetadataSize
		}
		m.datasetColumnMetadataTotalSize.WithLabelValues(sectionType).Observe(float64(totalColumnMetadataSize))
	}

	for i, column := range columns {
		columnType := column.Type.String()
		pages := columnPages[i]

		m.datasetColumnCompressedBytes.WithLabelValues(sectionType, columnType).Observe(float64(column.Info.CompressedSize))
		m.datasetColumnUncompressedBytes.WithLabelValues(sectionType, columnType).Observe(float64(column.Info.UncompressedSize))
		m.datasetColumnRows.WithLabelValues(sectionType, columnType).Observe(float64(column.Info.RowsCount))
		m.datasetColumnValues.WithLabelValues(sectionType, columnType).Observe(float64(column.Info.ValuesCount))

		m.datasetPageCount.WithLabelValues(sectionType, columnType).Observe(float64(len(pages)))

		for _, page := range pages {
			m.datasetPageCompressedBytes.WithLabelValues(sectionType, columnType).Observe(float64(page.Info.CompressedSize))
			m.datasetPageUncompressedBytes.WithLabelValues(sectionType, columnType).Observe(float64(page.Info.UncompressedSize))
			m.datasetPageRows.WithLabelValues(sectionType, columnType).Observe(float64(page.Info.RowsCount))
			m.datasetPageValues.WithLabelValues(sectionType, columnType).Observe(float64(page.Info.ValuesCount))
		}
	}

	return nil
}
