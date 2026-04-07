package index

import (
	"context"
	"io"
	"testing"
	"time"

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

func TestLabelPostingsCalculation_BasicPostings(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))

	ts1 := time.Unix(100, 0).UTC()
	ts2 := time.Unix(200, 0).UTC()
	ts3 := time.Unix(150, 0).UTC()

	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: []byte("hello")}, // svcA
		{StreamID: 2, Timestamp: ts2, Line: []byte("world")}, // svcB
		{StreamID: 1, Timestamp: ts3, Line: []byte("again")}, // svcA again
	}

	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	allPostings := readAllPostingsForTenant(t, builder, "tenant-1")

	// We expect 2 label postings: one for svcA, one for svcB.
	require.Len(t, allPostings, 2)

	// All should be label-kind.
	for _, p := range allPostings {
		require.Equal(t, postings.KindLabel, p.Kind)
		require.Equal(t, "service_name", p.ColumnName)
		require.NotNil(t, p.LabelValue)
	}

	// Find svcA and svcB.
	var svcAPosting, svcBPosting *postings.Posting
	for i := range allPostings {
		switch *allPostings[i].LabelValue {
		case "svcA":
			svcAPosting = &allPostings[i]
		case "svcB":
			svcBPosting = &allPostings[i]
		}
	}
	require.NotNil(t, svcAPosting, "expected svcA posting")
	require.NotNil(t, svcBPosting, "expected svcB posting")

	// svcA should have bit 1 set.
	require.NotEmpty(t, svcAPosting.StreamIDBitmap)
	// svcB should have bit 2 set.
	require.NotEmpty(t, svcBPosting.StreamIDBitmap)

	// Verify timestamps for svcA (min=ts1, max=ts3).
	require.Equal(t, ts1.UnixNano(), svcAPosting.MinTimestamp)
	require.Equal(t, ts3.UnixNano(), svcAPosting.MaxTimestamp)
}

func TestLabelPostingsCalculation_BitmapsNormalized(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))

	// Stream 1 = svcA, Stream 2 = svcB (from makeTestStreamLabels).
	batch := []logs.Record{
		{StreamID: 1, Timestamp: time.Unix(1, 0).UTC(), Line: []byte("a")}, // svcA
		{StreamID: 2, Timestamp: time.Unix(2, 0).UTC(), Line: []byte("b")}, // svcB
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	allPostings := readAllPostingsForTenant(t, builder, "tenant-1")
	require.Len(t, allPostings, 2)

	var svcAPosting, svcBPosting *postings.Posting
	for i := range allPostings {
		switch *allPostings[i].LabelValue {
		case "svcA":
			svcAPosting = &allPostings[i]
		case "svcB":
			svcBPosting = &allPostings[i]
		}
	}
	require.NotNil(t, svcAPosting)
	require.NotNil(t, svcBPosting)

	// Both bitmaps should be the same length after normalization.
	require.Equal(t, len(svcAPosting.StreamIDBitmap), len(svcBPosting.StreamIDBitmap),
		"both bitmaps should be normalized to the same size")

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
	calc := &labelPostingsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

	require.NoError(t, calc.Prepare(context.Background(), nil, logs.Stats{}))

	ts1 := time.Unix(10, 0).UTC()
	ts2 := time.Unix(20, 0).UTC()
	ts3 := time.Unix(30, 0).UTC()

	batch := []logs.Record{
		{StreamID: 1, Timestamp: ts1, Line: []byte("hello")},  // svcA, ts=10
		{StreamID: 1, Timestamp: ts2, Line: []byte("world!")}, // svcA, ts=20
		{StreamID: 1, Timestamp: ts3, Line: []byte("mid")},    // svcA, ts=30
	}
	require.NoError(t, calc.ProcessBatch(context.Background(), calcCtx, batch))
	require.NoError(t, calc.Flush(context.Background(), calcCtx))

	allPostings := readAllPostingsForTenant(t, builder, "tenant-1")
	require.Len(t, allPostings, 1) // Only svcA has records.

	p := allPostings[0]
	require.Equal(t, ts1.UnixNano(), p.MinTimestamp)
	require.Equal(t, ts3.UnixNano(), p.MaxTimestamp)
	expectedSize := int64(len("hello") + len("world!") + len("mid"))
	require.Equal(t, expectedSize, p.UncompressedSize)
}

func TestLabelPostingsCalculation_EmptyBatch(t *testing.T) {
	builder := newTestIndexBuilder(t)
	calcCtx := makeTestCalcContext(builder)
	calc := &labelPostingsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

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
	calc := &labelPostingsCalculation{sortSchemaKeys: defaultSortSchemaKeys}

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
	require.Len(t, allPostings, 2)

	// Verify that both svcA and svcB postings are present.
	labels := make(map[string]bool)
	for _, p := range allPostings {
		if p.LabelValue != nil {
			labels[*p.LabelValue] = true
		}
	}
	require.True(t, labels["svcA"])
	require.True(t, labels["svcB"])
}
