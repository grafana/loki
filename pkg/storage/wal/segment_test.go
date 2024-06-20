package wal

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"testing"
	"time"

	"github.com/dustin/go-humanize"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/logql/syntax"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb"
	"github.com/grafana/loki/v3/pkg/storage/wal/testdata"

	"github.com/grafana/loki/pkg/push"
)

func TestWalSegmentWriter_Append(t *testing.T) {
	type batch struct {
		tenant  string
		labels  string
		entries []*logproto.Entry
	}
	// Test cases
	tests := []struct {
		name     string
		batches  [][]batch
		expected []batch
	}{
		{
			name: "add two streams",
			batches: [][]batch{
				{
					{
						labels: "foo",
						tenant: "tenant1",
						entries: []*logproto.Entry{
							{Timestamp: time.Unix(1, 0), Line: "Entry 1"},
							{Timestamp: time.Unix(3, 0), Line: "Entry 3"},
						},
					},
					{
						labels: "bar",
						tenant: "tenant1",

						entries: []*logproto.Entry{
							{Timestamp: time.Unix(2, 0), Line: "Entry 2"},
							{Timestamp: time.Unix(3, 0), Line: "Entry 3"},
						},
					},
				},
				{
					{
						labels: "foo",
						tenant: "tenant1",

						entries: []*logproto.Entry{
							{Timestamp: time.Unix(2, 0), Line: "Entry 2"},
							{Timestamp: time.Unix(3, 0), Line: "Entry 3"},
						},
					},
					{
						labels: "bar",
						tenant: "tenant1",

						entries: []*logproto.Entry{
							{Timestamp: time.Unix(1, 0), Line: "Entry 1"},
						},
					},
				},
			},
			expected: []batch{
				{
					labels: "foo",
					tenant: "tenant1",
					entries: []*logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "Entry 1"},
						{Timestamp: time.Unix(2, 0), Line: "Entry 2"},
						{Timestamp: time.Unix(3, 0), Line: "Entry 3"},
						{Timestamp: time.Unix(3, 0), Line: "Entry 3"},
					},
				},
				{
					labels: "bar",
					tenant: "tenant1",
					entries: []*logproto.Entry{
						{Timestamp: time.Unix(1, 0), Line: "Entry 1"},
						{Timestamp: time.Unix(2, 0), Line: "Entry 2"},
						{Timestamp: time.Unix(3, 0), Line: "Entry 3"},
					},
				},
			},
		},
	}

	// Run the test cases
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// Create a new WalSegmentWriter
			w := NewWalSegmentWriter()
			// Append the entries
			for _, batch := range tt.batches {
				for _, stream := range batch {
					labels, err := syntax.ParseLabels(stream.labels)
					require.NoError(t, err)
					w.Append(stream.tenant, stream.labels, labels, stream.entries)
				}
			}
			require.NotEmpty(t, tt.expected, "expected entries are empty")
			// Check the entries
			for _, expected := range tt.expected {
				stream, ok := w.streams.Get(streamID{labels: expected.labels, tenant: expected.tenant})
				require.True(t, ok)
				lbs, err := syntax.ParseLabels(expected.labels)
				require.NoError(t, err)
				lbs = append(lbs, labels.Label{Name: string(tsdb.TenantLabel), Value: expected.tenant})
				sort.Sort(lbs)
				require.Equal(t, lbs, stream.lbls)
				require.Equal(t, expected.entries, stream.entries)
			}
		})
	}
}

func TestMultiTenantWrite(t *testing.T) {
	w := NewWalSegmentWriter()
	dst := bytes.NewBuffer(nil)

	lbls := []labels.Labels{
		labels.FromStrings("container", "foo", "namespace", "dev"),
		labels.FromStrings("container", "bar", "namespace", "staging"),
		labels.FromStrings("container", "bar", "namespace", "prod"),
	}

	tenants := []string{"z", "c", "a", "b"}

	for _, tenant := range tenants {
		for _, lbl := range lbls {
			lblString := lbl.String()
			for i := 0; i < 10; i++ {
				w.Append(tenant, lblString, lbl, []*push.Entry{
					{Timestamp: time.Unix(0, int64(i)), Line: fmt.Sprintf("log line %d", i)},
				})
			}
		}
	}
	n, err := w.WriteTo(dst)
	require.NoError(t, err)
	require.True(t, n > 0)

	r, err := NewReader(dst.Bytes())
	require.NoError(t, err)

	iter, err := r.Series(context.Background())
	require.NoError(t, err)

	var expectedSeries, actualSeries []string

	for _, tenant := range tenants {
		for _, lbl := range lbls {
			expectedSeries = append(expectedSeries, labels.NewBuilder(lbl).Set(tsdb.TenantLabel, tenant).Labels().String())
		}
	}

	for iter.Next() {
		actualSeries = append(actualSeries, iter.At().String())
		chk, err := iter.ChunkReader(nil)
		require.NoError(t, err)
		// verify all lines
		var i int
		for chk.Next() {
			ts, line := chk.At()
			require.Equal(t, int64(i), ts)
			require.Equal(t, fmt.Sprintf("log line %d", i), string(line))
			i++
		}
		require.NoError(t, chk.Err())
		require.NoError(t, chk.Close())
		require.Equal(t, 10, i)
	}
	require.NoError(t, iter.Err())
	require.ElementsMatch(t, expectedSeries, actualSeries)
}

func TestCompression(t *testing.T) {
	size := []int64{250 * 1024, 500 * 1024, 750 * 1024, 1 << 20, 2 << 20, 5 << 20, 10 << 20, 20 << 20, 50 << 20, 100 << 20}
	for _, s := range size {
		t.Run(fmt.Sprintf("size %.2f", float64(s)/(1024*1024)), func(t *testing.T) {
			testCompression(t, s)
		})
	}
}

func testCompression(t *testing.T, maxInputSize int64) {
	w := NewWalSegmentWriter()
	dst := bytes.NewBuffer(nil)
	files := testdata.Files()
	lbls := []labels.Labels{}
	generators := []*testdata.LogGenerator{}

	for _, file := range files {
		lbls = append(lbls, labels.FromStrings("filename", file, "namespace", "dev"))
		lbls = append(lbls, labels.FromStrings("filename", file, "namespace", "prod"))
		g := testdata.NewLogGenerator(t, file)
		generators = append(generators, g, g)
	}
	inputSize := int64(0)
	for inputSize < maxInputSize {
		for i, lbl := range lbls {
			more, line := generators[i].Next()
			if !more {
				continue
			}
			inputSize += int64(len(line))
			w.Append("tenant", lbl.String(), lbl, []*push.Entry{
				{Timestamp: time.Unix(0, int64(i*1e9)), Line: string(line)},
			})
		}
	}

	require.Equal(t, inputSize, w.InputSize())

	now := time.Now()
	n, err := w.WriteTo(dst)
	require.NoError(t, err)
	require.True(t, n > 0)
	compressionTime := time.Since(now)

	r, err := NewReader(dst.Bytes())
	require.NoError(t, err)
	inputSizeMB := float64(w.InputSize()) / (1024 * 1024)
	outputSizeMB := float64(dst.Len()) / (1024 * 1024)
	compressionRatio := (1 - (outputSizeMB / inputSizeMB)) * 100

	t.Logf("Input Size: %s\n", humanize.Bytes(uint64(w.InputSize())))
	t.Logf("Output Size: %s\n", humanize.Bytes(uint64(dst.Len())))
	t.Logf("Compression Ratio: %.2f%%\n", compressionRatio)
	t.Logf("Write time: %s\n", compressionTime)
	sizes, err := r.Sizes()
	require.NoError(t, err)
	t.Logf("Total chunks %d\n", len(sizes.Series))
	t.Logf("Index size  %s\n", humanize.Bytes(uint64(sizes.Index)))
	sizesString := ""
	for _, size := range sizes.Series {
		sizesString += humanize.Bytes(uint64(size)) + ", "
	}
	t.Logf("Series sizes: [%s]\n", sizesString)
}
