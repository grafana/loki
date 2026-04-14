package index

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/index/indexobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/logs"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/postings"
)

// readAllPostingsForTenant reads all postings from the builder for a given tenant.
func readAllPostingsForTenant(t *testing.T, builder *indexobj.Builder, tenantID string) []postings.Posting {
	t.Helper()
	pb := builder.PostingsBuilderForTenant(tenantID)
	if pb == nil {
		return nil
	}
	sections, err := pb.Flush(context.Background())
	require.NoError(t, err)

	var allPostings []postings.Posting
	for _, sec := range sections {
		rr, err := postings.NewRowReader(&sec)
		require.NoError(t, err)
		defer rr.Close()

		buf := make([]postings.Posting, 64)
		for {
			n, err := rr.Read(context.Background(), buf)
			allPostings = append(allPostings, buf[:n]...)
			if err == io.EOF {
				break
			}
			require.NoError(t, err)
		}
	}
	return allPostings
}

// findPosting returns the first posting matching the given column name and label value.
func findPosting(pp []postings.Posting, columnName, labelValue string) *postings.Posting {
	for i := range pp {
		if pp[i].ColumnName == columnName && pp[i].LabelValue == labelValue {
			return &pp[i]
		}
	}
	return nil
}

func TestLabelPostingsCalculation_BasicPostings(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))

	ts1 := time.Unix(100, 0).UTC()
	ts2 := time.Unix(200, 0).UTC()
	ts3 := time.Unix(150, 0).UTC()

	// Stream 1: service_name=svcA, env=prod
	// Stream 2: service_name=svcB, env=dev
	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: []byte("hello")}, // svcA, prod
		{StreamID: 2, Timestamp: ts2, Line: []byte("world")}, // svcB, dev
		{StreamID: 1, Timestamp: ts3, Line: []byte("again")}, // svcA, prod again
	}

	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	allPostings := readAllPostingsForTenant(t, builder, "tenant-1")

	// We expect 4 label postings: service_name=svcA, service_name=svcB, env=prod, env=dev.
	require.Len(t, allPostings, 4)

	// All should be label-kind.
	for _, p := range allPostings {
		require.Equal(t, postings.KindLabel, p.Kind)
		require.NotEmpty(t, p.LabelValue)
	}

	// Find svcA and svcB.
	svcAPosting := findPosting(allPostings, "service_name", "svcA")
	svcBPosting := findPosting(allPostings, "service_name", "svcB")
	require.NotNil(t, svcAPosting, "expected svcA posting")
	require.NotNil(t, svcBPosting, "expected svcB posting")

	// svcA should have bit 1 set.
	require.NotEmpty(t, svcAPosting.StreamIDBitmap)
	// svcB should have bit 2 set.
	require.NotEmpty(t, svcBPosting.StreamIDBitmap)

	// Verify timestamps for svcA (min=ts1, max=ts3).
	require.Equal(t, ts1.UnixNano(), svcAPosting.MinTimestamp)
	require.Equal(t, ts3.UnixNano(), svcAPosting.MaxTimestamp)

	// Verify env labels are also present.
	envProd := findPosting(allPostings, "env", "prod")
	envDev := findPosting(allPostings, "env", "dev")
	require.NotNil(t, envProd, "expected env=prod posting")
	require.NotNil(t, envDev, "expected env=dev posting")
}

func TestLabelPostingsCalculation_BitmapsNormalized(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))

	// Stream 1 = svcA/prod, Stream 2 = svcB/dev (from makeTestStreamLabels).
	batch := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(1, 0).UTC(), Line: []byte("a")}, // svcA, prod
		{StreamID: 2, Timestamp: time.Unix(2, 0).UTC(), Line: []byte("b")}, // svcB, dev
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	allPostings := readAllPostingsForTenant(t, builder, "tenant-1")
	// 4 postings: service_name=svcA, service_name=svcB, env=prod, env=dev
	require.Len(t, allPostings, 4)

	svcAPosting := findPosting(allPostings, "service_name", "svcA")
	svcBPosting := findPosting(allPostings, "service_name", "svcB")
	require.NotNil(t, svcAPosting)
	require.NotNil(t, svcBPosting)

	// All bitmaps should be the same length after normalization.
	for i := 1; i < len(allPostings); i++ {
		require.Equal(t, len(allPostings[0].StreamIDBitmap), len(allPostings[i].StreamIDBitmap),
			"all bitmaps should be normalized to the same size")
	}

	// svcA posting should include stream 1 but not stream 2.
	require.True(t, isBitSet(svcAPosting.StreamIDBitmap, 1), "svcA bitmap should have bit 1 set")
	require.False(t, isBitSet(svcAPosting.StreamIDBitmap, 2), "svcA bitmap should not have bit 2 set")

	// svcB posting should include stream 2 but not stream 1.
	require.True(t, isBitSet(svcBPosting.StreamIDBitmap, 2), "svcB bitmap should have bit 2 set")
	require.False(t, isBitSet(svcBPosting.StreamIDBitmap, 1), "svcB bitmap should not have bit 1 set")
}

// isBitSet checks if bit n is set in a LSB-numbered bitmap (Arrow convention).
func isBitSet(bitmap []byte, n int) bool {
	if n/8 >= len(bitmap) {
		return false
	}
	return bitmap[n/8]&(1<<(n%8)) != 0
}

func TestLabelPostingsCalculation_TimestampsAndSizes(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))

	ts1 := time.Unix(10, 0).UTC()
	ts2 := time.Unix(20, 0).UTC()
	ts3 := time.Unix(30, 0).UTC()

	// All records are stream 1 (service_name=svcA, env=prod).
	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: []byte("hello")},  // svcA, ts=10
		{StreamID: 1, Timestamp: ts2, Line: []byte("world!")}, // svcA, ts=20
		{StreamID: 1, Timestamp: ts3, Line: []byte("mid")},    // svcA, ts=30
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	allPostings := readAllPostingsForTenant(t, builder, "tenant-1")
	// 2 postings: service_name=svcA, env=prod (both from stream 1).
	require.Len(t, allPostings, 2)

	// Check timestamps and sizes on the service_name=svcA posting.
	p := findPosting(allPostings, "service_name", "svcA")
	require.NotNil(t, p)
	require.Equal(t, ts1.UnixNano(), p.MinTimestamp)
	require.Equal(t, ts3.UnixNano(), p.MaxTimestamp)
	expectedSize := int64(len("hello") + len("world!") + len("mid"))
	require.Equal(t, expectedSize, p.UncompressedSize)
}

func TestLabelPostingsCalculation_EmptyBatch(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, nil))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	// No data → no postings builder created.
	pb := builder.PostingsBuilderForTenant("tenant-1")
	require.Nil(t, pb, "expected no postings builder for empty batch")
}

func TestLabelPostingsCalculation_MultipleBatches(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))

	batch1 := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(1, 0).UTC(), Line: []byte("a")},
	}
	batch2 := []logs.Record{
		{StreamID: 2, Timestamp: time.Unix(2, 0).UTC(), Line: []byte("bb")},
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch1))
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch2))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	allPostings := readAllPostingsForTenant(t, builder, "tenant-1")
	// 4 postings: service_name=svcA, service_name=svcB, env=prod, env=dev
	require.Len(t, allPostings, 4)

	// Verify that both svcA and svcB postings are present.
	require.NotNil(t, findPosting(allPostings, "service_name", "svcA"))
	require.NotNil(t, findPosting(allPostings, "service_name", "svcB"))
	// Verify env labels are also present.
	require.NotNil(t, findPosting(allPostings, "env", "prod"))
	require.NotNil(t, findPosting(allPostings, "env", "dev"))
}

// BenchmarkLabelPostingsCalculation_ProcessBatch benchmarks ProcessBatch with
// varying numbers of log records and distinct streams. This helps track
// performance since the calculation must iterate through all rows in a logs
// section.
func BenchmarkLabelPostingsCalculation_ProcessBatch(b *testing.B) {
	benchCases := []struct {
		records int
		streams int
	}{
		{records: 1_000, streams: 10},
		{records: 10_000, streams: 10},
		{records: 10_000, streams: 100},
		{records: 100_000, streams: 10},
		{records: 100_000, streams: 100},
		{records: 100_000, streams: 1_000},
	}

	for _, bc := range benchCases {
		b.Run(fmt.Sprintf("records=%d/streams=%d", bc.records, bc.streams), func(b *testing.B) {
			// Build stream labels map with multiple labels per stream.
			streamLabels := make(map[int64]labels.Labels, bc.streams)
			for i := range bc.streams {
				streamLabels[int64(i)] = labels.FromStrings(
					"service_name", fmt.Sprintf("svc-%d", i),
					"cluster", fmt.Sprintf("cluster-%d", i%5),
					"namespace", fmt.Sprintf("ns-%d", i%10),
				)
			}

			// Pre-build the batch: records are distributed round-robin across streams.
			batch := make([]logs.Record, bc.records)
			for i := range batch {
				batch[i] = logs.Record{
					StreamID:  int64(i % bc.streams),
					Timestamp: time.Unix(int64(i), 0).UTC(),
					Line:      []byte("log line payload for benchmarking"),
				}
			}

			b.ResetTimer()
			b.ReportAllocs()

			for range b.N {
				builder, err := indexobj.NewBuilder(testCalculatorConfig, nil)
				if err != nil {
					b.Fatal(err)
				}
				calcCtx := &logsCalculationContext{
					tenantID:     "bench-tenant",
					objectPath:   "bench/path",
					sectionIdx:   0,
					streamLabels: streamLabels,
					builder:      builder,
				}
				calc := &labelPostingsCalculation{}

				if err := calc.Prepare(context.Background(), nil, logs.Stats{}); err != nil {
					b.Fatal(err)
				}
				if err := calc.ProcessBatch(context.Background(), calcCtx, batch); err != nil {
					b.Fatal(err)
				}
				if err := calc.Flush(context.Background(), calcCtx); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
