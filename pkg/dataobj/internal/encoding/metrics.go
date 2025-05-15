package encoding

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
)

// Metrics instruments encoded data objects.
type Metrics struct {
	sectionsCount       prometheus.Histogram
	fileMetadataSize    prometheus.Histogram
	sectionMetadataSize *prometheus.HistogramVec
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
	return errors.Join(errs...)
}

// Unregister unregisters metrics from the provided Registerer.
func (m *Metrics) Unregister(reg prometheus.Registerer) {
	reg.Unregister(m.sectionsCount)
	reg.Unregister(m.fileMetadataSize)
	reg.Unregister(m.sectionMetadataSize)
}

// SectionObserver is invoked by [Metrics.Observe] for each section in the object.
type SectionObserver func(ctx context.Context, sr SectionReader) error

// Observe observes the data object statistics for the given [Decoder].
func (m *Metrics) Observe(ctx context.Context, dec Decoder, onSection SectionObserver) error {
	metadata, err := dec.Metadata(ctx)
	if err != nil {
		return err
	}

	// TODO(rfratto): our Decoder interface should be updated to not hide the
	// metadata types to avoid recreating them here.

	m.sectionsCount.Observe(float64(len(metadata.Sections)))
	m.fileMetadataSize.Observe(float64(proto.Size(&filemd.Metadata{Sections: metadata.Sections})))

	var errs []error

	for _, section := range metadata.Sections {
		typ, err := GetSectionType(metadata, section)
		if err != nil {
			errs = append(errs, fmt.Errorf("getting section type: %w", err))
			continue
		}

		m.sectionMetadataSize.WithLabelValues(typ.String()).Observe(float64(calculateMetadataSize(section)))

		if onSection != nil {
			onSection(ctx, dec.SectionReader(metadata, section))
		}
	}

	return errors.Join(errs...)
}

// calculateMetadataSize returns the size of metadata in a section, accounting
// for whether it's using the deprecated fields or the new layout.
func calculateMetadataSize(section *filemd.SectionInfo) uint64 {
	if section.GetLayout() != nil {
		// This will return zero if GetMetadata returns nil, which is correct as it
		// defines the section as having no metadata.
		return section.GetLayout().GetMetadata().GetLength()
	}

	// Fallback to the deprecated field.
	return section.MetadataSize //nolint:staticcheck // MetadataSize is deprecated but still used as a fallback.
}
