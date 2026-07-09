package executor

import (
	"context"
	"fmt"
	"math/rand/v2"
	"time"

	"github.com/apache/arrow-go/v18/arrow"
	"github.com/apache/arrow-go/v18/arrow/array"
	"github.com/apache/arrow-go/v18/arrow/memory"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/physical"
	"github.com/grafana/loki/v3/pkg/engine/internal/semconv"
	"github.com/grafana/loki/v3/pkg/engine/internal/types"
)

var dummyLabelValues = map[string][]string{
	"cluster":      {"prod-ap-northeast-0", "prod-ap-south-0", "prod-eu-west-5", "prod-eu-west-0", "prod-gb-south-1", "prod-sa-east-1", "prod-us-east-0"},
	"namespace":    {"loki-001", "loki-002", "loki-003", "loki-004", "loki-005", "loki-006", "loki-007", "loki-008", "loki-009", "loki-010"},
	"container":    {"frontend", "querier", "index-gateway", "sheduler-worker", "scheduler", "compactor", "distributor", "ingester"},
	"pod":          nil, // generated per-row
	"level":        {"info", "warn", "error", "debug", "trace"},
	"service_name": {"frontend", "querier", "index-gateway", "sheduler-worker", "scheduler", "compactor", "distributor", "ingester"},
}

// all generated per-row
var dummyMetadataValues = map[string][]string{
	"bytes_processed": nil,
	"duration":        nil,
	"trace_id":        nil,
}

type dummyLoadPipeline struct {
	node      *physical.DummyLoad
	allocator memory.Allocator
	schema    *arrow.Schema
	labels    []string

	opened         bool
	batchesEmitted int
}

var _ Pipeline = (*dummyLoadPipeline)(nil)

func newDummyLoadPipeline(node *physical.DummyLoad) *dummyLoadPipeline {
	labels := node.Labels
	if len(labels) == 0 {
		labels = physical.DefaultDummyLoadLabels
	}

	fields := []arrow.Field{
		semconv.FieldFromIdent(semconv.ColumnIdentTimestamp, true),
		semconv.FieldFromIdent(semconv.ColumnIdentMessage, true),
	}
	for _, lbl := range labels {
		fields = append(fields, semconv.FieldFromIdent(
			semconv.NewIdentifier(lbl, types.ColumnTypeLabel, types.Loki.String),
			true,
		))
	}

	return &dummyLoadPipeline{
		node:      node,
		allocator: memory.DefaultAllocator,
		schema:    arrow.NewSchema(fields, nil),
		labels:    labels,
	}
}

func (d *dummyLoadPipeline) Open(_ context.Context) error {
	d.opened = true
	return nil
}

func (d *dummyLoadPipeline) Read(ctx context.Context) (arrow.RecordBatch, error) {
	if !d.opened {
		return nil, errPipelineNotOpen
	}
	if d.batchesEmitted >= d.node.NumBatches {
		return nil, EOF
	}

	if d.node.SleepPerBatch > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(d.node.SleepPerBatch):
		}
	}

	rec := d.generateBatch()
	d.batchesEmitted++
	return rec, nil
}

func (d *dummyLoadPipeline) Close() {}

func (d *dummyLoadPipeline) generateBatch() arrow.RecordBatch {
	size := d.node.BatchSize
	if size <= 0 {
		size = 1
	}

	tsBuilder := array.NewTimestampBuilder(d.allocator, &arrow.TimestampType{Unit: arrow.Nanosecond, TimeZone: "UTC"})
	msgBuilder := array.NewStringBuilder(d.allocator)
	defer tsBuilder.Release()
	defer msgBuilder.Release()

	lblBuilders := make([]*array.StringBuilder, len(d.labels))
	for i := range lblBuilders {
		lblBuilders[i] = array.NewStringBuilder(d.allocator)
		defer lblBuilders[i].Release()
	}

	baseTime := time.Now()
	for i := 0; i < size; i++ {
		tsBuilder.Append(arrow.Timestamp(baseTime.Add(time.Duration(i) * time.Millisecond).UnixNano()))
		msgBuilder.Append(fmt.Sprintf("dummy log line %d rnd=%d", i, rand.Int64()))
		for j, lbl := range d.labels {
			lblBuilders[j].Append(dummyLabelValue(lbl))
		}
	}

	cols := make([]arrow.Array, 0, 2+len(d.labels))
	cols = append(cols, tsBuilder.NewArray(), msgBuilder.NewArray())
	for _, b := range lblBuilders {
		cols = append(cols, b.NewArray())
	}
	defer func() {
		for _, c := range cols {
			c.Release()
		}
	}()

	return array.NewRecordBatch(d.schema, cols, int64(size))
}

// dummyLabelValue returns a random value for the given label name.
func dummyLabelValue(label string) string {
	if pool, ok := dummyLabelValues[label]; ok && len(pool) > 0 {
		return pool[rand.IntN(len(pool))]
	}
	// Per-row generated values for labels without a fixed pool.
	switch label {
	case "bytes_processed":
		return fmt.Sprintf("%d", rand.Uint32())
	case "pod":
		return fmt.Sprintf("pod-%04x", rand.Uint32()&0xFFFF)
	default:
		return fmt.Sprintf("%s-%04x", label, rand.Uint32()&0xFF)
	}
}

// dummyMetadataValue returns a random value for the given metadata field name.
func dummyMetadataValue(field string) string {
	if pool, ok := dummyMetadataValues[field]; ok && len(pool) > 0 {
		return pool[rand.IntN(len(pool))]
	}
	// Per-row generated values for metadata fields without a fixed pool.
	switch field {
	case "bytes_processed":
		return fmt.Sprintf("%d", rand.Int64N(1_000_000))
	case "duration":
		return fmt.Sprintf("%dms", rand.Int64N(10_000))
	case "trace_id":
		return fmt.Sprintf("%016x%016x", rand.Uint64(), rand.Uint64())
	default:
		return fmt.Sprintf("%s-%04x", field, rand.Uint32()&0xFF)
	}
}
