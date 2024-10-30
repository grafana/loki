package tsdb

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/storage/stores/shipper/indexshipper/tsdb/index"
)

func Test_Build(t *testing.T) {
	setup := func(version int) (context.Context, *Builder, string) {
		builder := NewBuilder(version)
		tmpDir := t.TempDir()

		lbls1 := mustParseLabels(`{foo="bar", a="b"}`)

		stream := stream{
			labels: lbls1,
			fp:     model.Fingerprint(lbls1.Hash()),
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
		indexPath := fakeIdentifierPathForBounds(path, 1, 6) //default step is 1
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
