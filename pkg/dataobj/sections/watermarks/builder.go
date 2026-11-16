package watermarks

import (
	"cmp"
	"errors"
	"fmt"
	"slices"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	datasetmd_v2 "github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/internal/columnar"
)

// Watermark is the highest offset of one Kafka topic and partition whose
// records the ToC already holds.
//
// Topic must not be empty. Partition and Watermark must not be negative.
type Watermark struct {
	Topic     string
	Partition int32
	Watermark int64 // The offset itself, not the next offset to read.
}

// topicPartition identifies a Kafka partition.
type topicPartition struct {
	topic     string
	partition int32
}

// Builder builds a watermarks section. It holds one row per topic and
// partition.
type Builder struct {
	metrics      *Metrics
	pageSize     int
	pageRowCount int
	tenant       string

	watermarks map[topicPartition]int64
}

// NewBuilder creates a new Builder for tenant.
func NewBuilder(metrics *Metrics, tenant string, pageSize, pageRowCount int) *Builder {
	return &Builder{
		metrics:      metrics,
		pageSize:     pageSize,
		pageRowCount: pageRowCount,
		tenant:       tenant,
		watermarks:   make(map[topicPartition]int64),
	}
}

// Tenant returns the tenant that owns the section.
func (b *Builder) Tenant() string { return b.tenant }

// Type returns the section type of the watermarks section.
func (b *Builder) Type() dataobj.SectionType { return sectionType }

// Set stores the watermark of a topic and partition. A second call for the same
// topic and partition replaces the first watermark with a higher or equal one.
//
// Set returns an error, and stores nothing, if the topic is empty, if the
// partition or the watermark is negative, or if the watermark is lower than the
// one already stored. A watermark only moves forward, so a lower value points
// to a bug in the caller. The reader rejects negative rows, so the builder must
// not write them.
func (b *Builder) Set(topic string, partition int32, watermark int64) error {
	switch {
	case topic == "":
		return errors.New("watermark topic must not be empty")
	case partition < 0:
		return fmt.Errorf("watermark partition must not be negative: got %d", partition)
	case watermark < 0:
		return fmt.Errorf("watermark must not be negative: got %d", watermark)
	}

	key := topicPartition{topic: topic, partition: partition}
	if current, ok := b.watermarks[key]; ok && watermark < current {
		return fmt.Errorf("watermark of topic %q partition %d must not decrease: got %d, have %d", topic, partition, watermark, current)
	}
	b.watermarks[key] = watermark
	return nil
}

// EstimatedSize returns the estimated size of the watermarks section in
// bytes. It returns 0 when the builder holds no rows.
//
// The estimate counts the topic bytes, 8 bytes for each partition and
// watermark, and a fixed metadata overhead for each column. The delta encoding
// of the numbers makes the real size smaller, so the estimate is an upper
// bound.
func (b *Builder) EstimatedSize() int {
	if len(b.watermarks) == 0 {
		return 0
	}

	const (
		numbersSize      = 8 + 8
		metadataOverhead = 100
	)
	size := len(sectionColumns) * metadataOverhead
	for key := range b.watermarks {
		size += len(key.topic) + numbersSize
	}
	return size
}

// Flush writes the watermarks section to w. On success, Flush resets b.
func (b *Builder) Flush(w dataobj.SectionWriter) (n int64, err error) {
	timer := prometheus.NewTimer(b.metrics.encodeSeconds)
	defer timer.ObserveDuration()

	var enc columnar.Encoder
	defer enc.Reset()
	if err := b.encodeTo(&enc); err != nil {
		return 0, fmt.Errorf("building encoder: %w", err)
	}

	enc.SetTenant(b.tenant)

	n, err = enc.Flush(w)
	if err == nil {
		b.Reset()
	}
	return n, err
}

// sortedWatermarks returns the rows ordered by topic, then partition.
func (b *Builder) sortedWatermarks() []Watermark {
	rows := make([]Watermark, 0, len(b.watermarks))
	for key, watermark := range b.watermarks {
		rows = append(rows, Watermark{Topic: key.topic, Partition: key.partition, Watermark: watermark})
	}
	slices.SortFunc(rows, func(a, c Watermark) int {
		if r := cmp.Compare(a.Topic, c.Topic); r != 0 {
			return r
		}
		return cmp.Compare(a.Partition, c.Partition)
	})
	return rows
}

// Reset drops all rows. The tenant stays.
func (b *Builder) Reset() {
	clear(b.watermarks)
}

func (b *Builder) encodeTo(enc *columnar.Encoder) error {
	topicBuilder, err := b.newColumnBuilder("topic", ColumnTypeTopic, datasetmd_v2.PHYSICAL_TYPE_BINARY, datasetmd_v2.ENCODING_TYPE_PLAIN, datasetmd_v2.COMPRESSION_TYPE_ZSTD)
	if err != nil {
		return fmt.Errorf("creating topic column: %w", err)
	}
	partitionBuilder, err := b.newColumnBuilder("partition", ColumnTypePartition, datasetmd_v2.PHYSICAL_TYPE_INT64, datasetmd_v2.ENCODING_TYPE_DELTA, datasetmd_v2.COMPRESSION_TYPE_NONE)
	if err != nil {
		return fmt.Errorf("creating partition column: %w", err)
	}
	watermarkBuilder, err := b.newColumnBuilder("watermark", ColumnTypeWatermark, datasetmd_v2.PHYSICAL_TYPE_INT64, datasetmd_v2.ENCODING_TYPE_DELTA, datasetmd_v2.COMPRESSION_TYPE_NONE)
	if err != nil {
		return fmt.Errorf("creating watermark column: %w", err)
	}

	var errs []error
	for i, wm := range b.sortedWatermarks() {
		errs = append(errs,
			topicBuilder.Append(i, dataset.BinaryValue([]byte(wm.Topic))),
			partitionBuilder.Append(i, dataset.Int64Value(int64(wm.Partition))),
			watermarkBuilder.Append(i, dataset.Int64Value(wm.Watermark)),
		)
	}

	errs = append(errs, encodeColumn(enc, ColumnTypeTopic, topicBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypePartition, partitionBuilder))
	errs = append(errs, encodeColumn(enc, ColumnTypeWatermark, watermarkBuilder))
	if err := errors.Join(errs...); err != nil {
		return fmt.Errorf("encoding columns: %w", err)
	}

	return nil
}

func (b *Builder) newColumnBuilder(
	name string,
	columnType ColumnType,
	physical datasetmd_v2.PhysicalType,
	encoding datasetmd_v2.EncodingType,
	compression datasetmd_v2.CompressionType,
) (*dataset.ColumnBuilder, error) {
	return dataset.NewColumnBuilder(name, dataset.BuilderOptions{
		PageSizeHint:    b.pageSize,
		PageMaxRowCount: b.pageRowCount,
		Type: dataset.ColumnType{
			Physical: physical,
			Logical:  columnType.String(),
		},
		Encoding:    encoding,
		Compression: compression,
		Statistics: dataset.StatisticsOptions{
			StoreRangeStats: true,
		},
	})
}

func encodeColumn(enc *columnar.Encoder, columnType ColumnType, builder *dataset.ColumnBuilder) error {
	column, err := builder.Flush()
	if err != nil {
		return fmt.Errorf("flushing %s column: %w", columnType, err)
	}

	columnEnc, err := enc.OpenColumn(column.ColumnDesc())
	if err != nil {
		return fmt.Errorf("opening %s column encoder: %w", columnType, err)
	}
	defer func() {
		// Discard releases the column if Commit did not run. Discard fails after
		// Commit, so the code ignores its error.
		_ = columnEnc.Discard()
	}()
	if len(column.Pages) == 0 {
		// The column has no pages, so the deferred Discard drops it.
		return nil
	}

	for _, page := range column.Pages {
		if err := columnEnc.AppendPage(page); err != nil {
			return fmt.Errorf("appending %s page: %w", columnType, err)
		}
	}

	return columnEnc.Commit()
}
