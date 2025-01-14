package encoding_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

func Test(t *testing.T) {
	type Country struct {
		Name    string
		Capital string
	}

	countries := []Country{
		{"India", "New Delhi"},
		{"USA", "Washington D.C."},
		{"UK", "London"},
		{"France", "Paris"},
		{"Germany", "Berlin"},
	}

	var buf bytes.Buffer

	t.Run("Encode", func(t *testing.T) {
		nameBuilder, err := dataset.NewColumnBuilder("name", dataset.BuilderOptions{
			Value:       datasetmd.VALUE_TYPE_STRING,
			Encoding:    datasetmd.ENCODING_TYPE_PLAIN,
			Compression: datasetmd.COMPRESSION_TYPE_NONE,
		})
		require.NoError(t, err)

		capitalBuilder, err := dataset.NewColumnBuilder("capital", dataset.BuilderOptions{
			Value:       datasetmd.VALUE_TYPE_STRING,
			Encoding:    datasetmd.ENCODING_TYPE_PLAIN,
			Compression: datasetmd.COMPRESSION_TYPE_NONE,
		})
		require.NoError(t, err)

		for i, c := range countries {
			require.NoError(t, nameBuilder.Append(i, dataset.StringValue(c.Name)))
			require.NoError(t, capitalBuilder.Append(i, dataset.StringValue(c.Capital)))
		}

		nameColumn, err := nameBuilder.Flush()
		require.NoError(t, err)
		capitalColumn, err := capitalBuilder.Flush()
		require.NoError(t, err)

		enc := encoding.NewEncoder(&buf)
		streamsEnc, err := enc.OpenStreams()
		require.NoError(t, err)

		colEnc, err := streamsEnc.OpenColumn(streamsmd.COLUMN_TYPE_LABEL, &nameColumn.Info)
		require.NoError(t, err)
		for _, page := range nameColumn.Pages {
			require.NoError(t, colEnc.AppendPage(page))
		}
		require.NoError(t, colEnc.Commit())

		colEnc, err = streamsEnc.OpenColumn(streamsmd.COLUMN_TYPE_LABEL, &capitalColumn.Info)
		require.NoError(t, err)
		for _, page := range capitalColumn.Pages {
			require.NoError(t, colEnc.AppendPage(page))
		}
		require.NoError(t, colEnc.Commit())

		require.NoError(t, streamsEnc.Commit())
		require.NoError(t, enc.Flush())
	})

	t.Run("Decode", func(t *testing.T) {
		dec := encoding.ReadSeekerDecoder(bytes.NewReader(buf.Bytes()))
		sections, err := dec.Sections(context.TODO())
		require.NoError(t, err)
		require.Len(t, sections, 1)
		require.Equal(t, filemd.SECTION_TYPE_STREAMS, sections[0].Type)

		dset := encoding.StreamsDataset(dec.StreamsDecoder(), sections[0])

		columns, err := result.Collect(dset.ListColumns(context.Background()))
		require.NoError(t, err)

		var actual []Country

		for result := range dataset.Iter(context.Background(), columns) {
			row, err := result.Value()
			require.NoError(t, err)
			require.Len(t, row.Values, 2)
			require.Equal(t, len(actual), row.Index)

			actual = append(actual, Country{
				Name:    row.Values[0].String(),
				Capital: row.Values[1].String(),
			})
		}

		require.Equal(t, countries, actual)
	})
}
