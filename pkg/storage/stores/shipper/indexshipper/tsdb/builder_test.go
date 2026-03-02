package tsdb

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index/sectionref"
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

	getReader := func(path string) *index.Reader {
		indexPath := fakeIdentifierPathForBounds(path, 1, 6) // default step is 1
		files, err := filepath.Glob(indexPath)
		require.NoError(t, err)
		require.Len(t, files, 1)

		reader, err := index.NewFileReader(files[0])
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
		indexPath := fakeIdentifierPathForBounds(tmpDir, 1, 6) // default step is 1

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

func TestBuilderSectionRefs(t *testing.T) {
	lbls := mustParseLabels(`{foo="bar"}`)
	fp := model.Fingerprint(labels.StableHash(lbls))

	t.Run("legacy add series path remains unchanged", func(t *testing.T) {
		builder := NewBuilder(index.FormatV3)
		builder.AddSeries(lbls, fp, []index.ChunkMeta{{Checksum: 99, MinTime: 1, MaxTime: 2}})

		require.Nil(t, builder.SectionRefTable())
		data, err := builder.SectionRefTableBytes()
		require.NoError(t, err)
		require.Nil(t, data)
	})

	t.Run("assigns checksums via section reference table", func(t *testing.T) {
		builder := NewBuilder(index.FormatV3)

		refA := sectionref.SectionRef{Path: "obj/a", SectionID: 1, SeriesID: 0}
		refB := sectionref.SectionRef{Path: "obj/b", SectionID: 2, SeriesID: 1}

		err := builder.AddSeriesWithSectionRefs(
			lbls,
			fp,
			[]sectionref.SectionMeta{
				{SectionRef: refB, ChunkMeta: index.ChunkMeta{MinTime: 1, MaxTime: 2, KB: 10, Entries: 20}},
				{SectionRef: refA, ChunkMeta: index.ChunkMeta{MinTime: 3, MaxTime: 4, KB: 11, Entries: 21}},
				{SectionRef: refB, ChunkMeta: index.ChunkMeta{MinTime: 5, MaxTime: 6, KB: 12, Entries: 22}},
			},
		)
		require.NoError(t, err)

		id := lbls.String()
		require.Contains(t, builder.streams, id)
		require.Len(t, builder.streams[id].chunks, 3)

		dst := builder.SectionRefTable()
		require.NotNil(t, dst)
		require.Equal(t, 2, dst.Len())

		remappedB := builder.streams[id].chunks[0].Checksum
		remappedA := builder.streams[id].chunks[1].Checksum
		require.NotEqual(t, remappedA, remappedB)
		require.Equal(t, remappedB, builder.streams[id].chunks[2].Checksum)
		require.Equal(t, int64(1), builder.streams[id].chunks[0].MinTime)
		require.Equal(t, uint32(22), builder.streams[id].chunks[2].Entries)

		gotA, ok := dst.Lookup(remappedA)
		require.True(t, ok)
		require.Equal(t, refA, gotA)

		gotB, ok := dst.Lookup(remappedB)
		require.True(t, ok)
		require.Equal(t, refB, gotB)
	})
}
