package dataobj

import (
	"errors"
	"fmt"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/prometheus/client_golang/prometheus"
)

// Metrics instruments encoded data objects.
type Metrics struct {
	sectionsCount           prometheus.Histogram
	lastObjectSectionsCount prometheus.Gauge
	lastObjectTenantsCount  prometheus.Gauge
	fileMetadataSize        prometheus.Histogram
	sectionMetadataSize     *prometheus.HistogramVec
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

		lastObjectSectionsCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "last_object_sections_count",
			Help:      "Number of sections in the last encoded data object.",
		}),

		lastObjectTenantsCount: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "loki_dataobj",
			Subsystem: "encoding",
			Name:      "last_object_tenants_count",
			Help:      "Number of tenants in the last encoded data object.",
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
	errs = append(errs, reg.Register(m.lastObjectSectionsCount))
	errs = append(errs, reg.Register(m.lastObjectTenantsCount))
	errs = append(errs, reg.Register(m.fileMetadataSize))
	errs = append(errs, reg.Register(m.sectionMetadataSize))
	return errors.Join(errs...)
}

// Unregister unregisters metrics from the provided Registerer.
func (m *Metrics) Unregister(reg prometheus.Registerer) {
	reg.Unregister(m.sectionsCount)
	reg.Unregister(m.lastObjectSectionsCount)
	reg.Unregister(m.lastObjectTenantsCount)
	reg.Unregister(m.fileMetadataSize)
	reg.Unregister(m.sectionMetadataSize)
}

// Observe updates metrics with statistics about the given [Object].
func (m *Metrics) Observe(obj *Object) error {
	numSections := float64(len(obj.sections))
	m.sectionsCount.Observe(numSections)
	m.lastObjectSectionsCount.Set(numSections)
	tenants := make(map[string]struct{})
	for _, sec := range obj.sections {
		tenants[sec.Tenant] = struct{}{}
	}
	m.lastObjectTenantsCount.Set(float64(len(tenants)))
	m.fileMetadataSize.Observe(float64(proto.Size(obj.metadata)))

	var errs []error

	for _, section := range obj.metadata.Sections {
		typ, err := getSectionType(obj.metadata, section)
		if err != nil {
			errs = append(errs, fmt.Errorf("getting section type: %w", err))
			continue
		}

		metadataSize := section.GetLayout().GetMetadata().GetLength()
		m.sectionMetadataSize.WithLabelValues(typ.String()).Observe(float64(metadataSize))
	}

	return errors.Join(errs...)
}
