package tsdb

import (
	"context"
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v4/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func Test_Build(t *testing.T) {
	setup := func(version int) (context.Context, *Builder, string) {
		builder := NewBuilder(version)
		tmpDir := t.TempDir()

		lbls1 := mustParseLabels(`{foo="bar", a="b"}`)

		stream := stream{
			labels: lbls1,
			fp:     model.Fingerprint(labels.StableHash(lbls1)),
			chunks: buildChunkMetas(1, 5),
		}

		builder.AddSeries(
			lbls1,
			stream.fp,
			stream.chunks,
		)

		return context.Background(), builder, tmpDir
	}

	getReader := func(path string) *index.ByteSliceReader {
		indexPath := fakeIdentifierPathForBounds(path, 1, 6) //default step is 1
		files, err := filepath.Glob(indexPath)
		require.NoError(t, err)
		require.Len(t, files, 1)

		reader, err := index.NewMmapFileReader(files[0])
		require.NoError(t, err)
		return reader
	}

	t.Run("writes index to disk with from/through bounds of series in filename", func(t *testing.T) {
		ctx, builder, tmpDir := setup(index.FormatV3)

		_, err := builder.Build(ctx, tmpDir, func(from, through model.Time, checksum uint32) Identifier {
			return &fakeIdentifier{
				parentPath: tmpDir,
				from:       from,
				through:    through,
				checksum:   checksum,
			}
		})

		require.NoError(t, err)
		indexPath := fakeIdentifierPathForBounds(tmpDir, 1, 6) //default step is 1

		files, err := filepath.Glob(indexPath)
		require.NoError(t, err)
		require.Len(t, files, 1)
	})

	t.Run("sorts symbols before writing to the index", func(t *testing.T) {
		ctx, builder, tmpDir := setup(index.FormatV3)
		_, err := builder.Build(ctx, tmpDir, func(from, through model.Time, checksum uint32) Identifier {
			return &fakeIdentifier{
				parentPath: tmpDir,
				from:       from,
				through:    through,
				checksum:   checksum,
			}
		})
		require.NoError(t, err)

		reader := getReader(tmpDir)

		symbols := reader.Symbols()
		require.NoError(t, err)

		symbolsList := make([]string, 0, 2)
		for symbols.Next() {
			symbolsList = append(symbolsList, symbols.At())
		}

		require.Equal(t, symbolsList, []string{"a", "b", "bar", "foo"})
	})

	t.Run("write index with correct version", func(t *testing.T) {
		ctx, builder, tmpDir := setup(index.FormatV2)
		_, err := builder.Build(ctx, tmpDir, func(from, through model.Time, checksum uint32) Identifier {
			return &fakeIdentifier{
				parentPath: tmpDir,
				from:       from,
				through:    through,
				checksum:   checksum,
			}
		})
		require.NoError(t, err)
		reader := getReader(tmpDir)
		require.Equal(t, index.FormatV2, reader.Version())
	})
}

type fakeIdentifier struct {
	parentPath string
	from       model.Time
	through    model.Time
	checksum   uint32
}

func (f *fakeIdentifier) Name() string {
	return "need to implement Name() function for fakeIdentifier"
}

func (f *fakeIdentifier) Path() string {
	path := fmt.Sprintf("%d-%d-%x.tsdb", f.from, f.through, f.checksum)
	return filepath.Join(f.parentPath, path)
}

func fakeIdentifierPathForBounds(path string, from, through model.Time) string {
	return filepath.Join(path, fmt.Sprintf("%d-%d-*.tsdb", from, through))
}

func TestBuilder_HashCollisions(t *testing.T) {
	lbls := []labels.Labels{
		mustParseLabels(`{foo="a"}`),
		mustParseLabels(`{foo="b"}`),
		mustParseLabels(`{foo="c"}`),
	}

	for _, tc := range []struct {
		name string
		hash func(labels.Labels) uint64
	}{
		{name: "real hash", hash: labels.Labels.Hash},
		{name: "all streams collide", hash: func(labels.Labels) uint64 { return 1 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBuilder(index.FormatV3)
			b.hash = tc.hash

			for i, ls := range lbls {
				b.AddSeries(ls, model.Fingerprint(i), buildChunkMetas(1, 2))
				// a repeated series must be merged into the existing stream
				b.AddSeries(ls, model.Fingerprint(i), buildChunkMetas(3, 4))
			}
			b.FinalizeChunks()
			require.Equal(t, len(lbls), b.numStreams())

			for _, ls := range lbls {
				s := b.getStream(ls)
				require.NotNil(t, s)
				require.Equal(t, ls, s.labels)
				require.Len(t, s.chunks, 4)

				has, err := b.HasChunk(ls, s.chunks[0])
				require.NoError(t, err)
				require.True(t, has)
			}
			require.Nil(t, b.getStream(mustParseLabels(`{foo="d"}`)))

			// empty out the first stream so it gets dropped, leaving any streams that collide with it findable
			for _, chk := range slices.Clone(b.getStream(lbls[0]).chunks) {
				found, err := b.DropChunk(lbls[0], chk)
				require.NoError(t, err)
				require.True(t, found)
			}
			b.dropEmptyStreams()
			require.Equal(t, len(lbls)-1, b.numStreams())
			require.Nil(t, b.getStream(lbls[0]))

			require.True(t, b.deleteStream(lbls[2]))
			require.False(t, b.deleteStream(lbls[2]))
			require.Equal(t, 1, b.numStreams())
			require.NotNil(t, b.getStream(lbls[1]))

			require.NoError(t, b.InsertChunk(lbls[1], index.ChunkMeta{MinTime: 10, MaxTime: 11}))
			require.Len(t, b.getStream(lbls[1]).chunks, 5)
			require.Error(t, b.InsertChunk(lbls[0], index.ChunkMeta{MinTime: 10, MaxTime: 11}))
		})
	}
}

func BenchmarkBuilder(b *testing.B) {
	const numSeries = 100_000
	series := make([]labels.Labels, numSeries)
	for i := range series {
		series[i] = labels.FromStrings(
			"cluster", fmt.Sprintf("cluster-%d", i%10),
			"container", fmt.Sprintf("container-%d", i%300),
			"namespace", fmt.Sprintf("namespace-%d", i%50),
			"pod", fmt.Sprintf("pod-%d-7f9c8d6b5-%05d", i%300, i),
			"service_name", fmt.Sprintf("service-%d", i%300),
			"stream", []string{"stdout", "stderr"}[i%2],
		)
	}
	chks := buildChunkMetas(1, 1)
	addSeries := func() *Builder {
		builder := NewBuilder(index.FormatV3)
		for i, ls := range series {
			// the compactor copies labels out of the source index before adding them
			builder.AddSeries(ls.Copy(), model.Fingerprint(i), chks)
		}
		return builder
	}

	b.Run("AddSeries", func(b *testing.B) {
		b.ReportAllocs()
		var retained uint64
		for b.Loop() {
			b.StopTimer()
			before := heapAlloc()
			b.StartTimer()

			builder := addSeries()

			b.StopTimer()
			retained = heapAlloc() - before
			runtime.KeepAlive(builder)
			b.StartTimer()
		}
		b.ReportMetric(float64(retained)/numSeries, "retained-B/series")
	})

	b.Run("Build", func(b *testing.B) {
		b.ReportAllocs()
		createFn := func(from, through model.Time, checksum uint32) Identifier {
			return &fakeIdentifier{from: from, through: through, checksum: checksum}
		}
		for b.Loop() {
			builder := addSeries()
			builder.FinalizeChunks()
			_, _, err := builder.BuildInMemory(context.Background(), createFn)
			require.NoError(b, err)
		}
	})
}

func heapAlloc() uint64 {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}
