package indexpointers_test

import (
	"errors"
	"io"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
	"github.com/grafana/loki/v3/pkg/memory"
)

const benchPathVariants = 200

type readerBenchParams struct {
	name        string
	numPointers int
}

func BenchmarkReaderRead(b *testing.B) {
	benchmarks := []readerBenchParams{
		{
			name:        "1k pointers",
			numPointers: 1000,
		},
		{
			name:        "10k pointers",
			numPointers: 10000,
		},
		{
			name:        "100k pointers",
			numPointers: 100000,
		},
	}

	for _, bm := range benchmarks {
		b.Run(bm.name, func(b *testing.B) {
			benchmarkReaderRead(b, bm)
		})
	}
}

func benchmarkReaderRead(b *testing.B, params readerBenchParams) {
	ctx := b.Context()
	sec := buildBenchSection(b, params.numPointers)
	columns := sec.Columns()

	opts := indexpointers.ReaderOptions{
		Columns: columns,
	}

	reader := indexpointers.NewReader(opts)
	alloc := memory.NewAllocator(nil)
	defer alloc.Free()

	b.ReportAllocs()

	for b.Loop() {
		reader.Reset(opts)
		require.NoError(b, reader.Open(ctx))
		totalRows := 0

		for {
			alloc.Reclaim()
			batch, err := reader.ReadRecordBatch(ctx, alloc, 256)
			if errors.Is(err, io.EOF) {
				break
			}
			require.NoError(b, err)
			totalRows += batch.Rows
		}

		require.NoError(b, reader.Close())

		if totalRows == 0 {
			b.Fatal("read 0 rows")
		}
	}

	b.StopTimer()
}

func buildBenchSection(b *testing.B, numPointers int) *indexpointers.Section {
	b.Helper()

	sectionBuilder := indexpointers.NewBuilder(nil, 0, 2)

	for i := 0; i < numPointers; i++ {
		path := "path/" + strconv.Itoa(i%benchPathVariants)
		startTs := unixTime(int64(i * 2))
		endTs := unixTime(int64(i*2 + 1))
		sectionBuilder.Append(indexpointers.IndexPointer{Path: path, StartTs: startTs, EndTs: endTs})
	}

	objectBuilder := dataobj.NewBuilder(nil)
	require.NoError(b, objectBuilder.Append(sectionBuilder))

	obj, closer, err := objectBuilder.Flush()
	require.NoError(b, err)
	b.Cleanup(func() { require.NoError(b, closer.Close()) })

	sec, err := indexpointers.Open(b.Context(), obj.Sections()[0])
	require.NoError(b, err)
	return sec
}
