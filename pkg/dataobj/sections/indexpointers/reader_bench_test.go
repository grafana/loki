package indexpointers_test

import (
	"context"
	"errors"
	"io"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
	"github.com/grafana/loki/v3/pkg/dataobj/sections/indexpointers"
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
	ctx := context.Background()
	sec := buildBenchSection(b, params.numPointers)
	columns := sec.Columns()

	opts := indexpointers.ReaderOptions{
		Columns: columns,
	}

	reader := indexpointers.NewReader(opts)

	b.ReportAllocs()

	for b.Loop() {
		reader.Reset(opts)
		require.NoError(b, reader.Open(ctx))
		totalRows := int64(0)

		for {
			rec, err := reader.Read(ctx, 256)
			if rec != nil {
				totalRows += rec.NumRows()
				rec.Release()
			}

			if err != nil {
				if errors.Is(err, io.EOF) {
					break
				}
				require.NoError(b, err)
			}
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
		sectionBuilder.Append(path, startTs, endTs)
	}

	objectBuilder := dataobj.NewBuilder(nil)
	require.NoError(b, objectBuilder.Append(sectionBuilder))

	obj, closer, err := objectBuilder.Flush()
	require.NoError(b, err)
	b.Cleanup(func() { closer.Close() })

	sec, err := indexpointers.Open(context.Background(), obj.Sections()[0])
	require.NoError(b, err)
	return sec
}
