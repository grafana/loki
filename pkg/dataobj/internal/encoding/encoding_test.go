package encoding_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/filemd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

func TestStreams(t *testing.T) {
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

	t.Run("Metrics", func(t *testing.T) {
		dec := encoding.ReaderAtDecoder(bytes.NewReader(buf.Bytes()), int64(buf.Len()))

		metrics := encoding.NewMetrics()
		require.NoError(t, metrics.Register(prometheus.NewRegistry()))
		require.NoError(t, metrics.Observe(context.Background(), dec))
	})

	t.Run("Decode", func(t *testing.T) {
		dec := encoding.ReaderAtDecoder(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		sections, err := dec.Sections(context.TODO())
		require.NoError(t, err)
		require.Len(t, sections, 1)
		require.Equal(t, filemd.SECTION_TYPE_STREAMS, sections[0].Type)

		dset := encoding.StreamsDataset(dec.StreamsDecoder(), sections[0])

		columns, err := result.Collect(dset.ListColumns(context.Background()))
		require.NoError(t, err)

		var actual []Country

		r := dataset.NewReader(dataset.ReaderOptions{
			Dataset: dset,
			Columns: columns,
		})

		buf := make([]dataset.Row, 1024)
		for {
			n, err := r.Read(context.Background(), buf)
			if err != nil && !errors.Is(err, io.EOF) {
				require.NoError(t, err)
			} else if n == 0 && errors.Is(err, io.EOF) {
				break
			}

			for _, row := range buf[:n] {
				require.Len(t, row.Values, 2)
				require.Equal(t, len(actual), row.Index)

				actual = append(actual, Country{
					Name:    row.Values[0].String(),
					Capital: row.Values[1].String(),
				})
			}
		}

		require.Equal(t, countries, actual)
	})
}

func TestLogs(t *testing.T) {
	var (
		columnA = &dataset.MemColumn{
			Pages: []*dataset.MemPage{
				{Data: []byte("Hello")},
				{Data: []byte("World")},
				{Data: []byte("!")},
			},
		}
		columnB = &dataset.MemColumn{
			Pages: []*dataset.MemPage{
				{Data: []byte("metadata")},
				{Data: []byte("column")},
			},
		}
	)

	var buf bytes.Buffer

	t.Run("Encode", func(t *testing.T) {
		enc := encoding.NewEncoder(&buf)
		logsEnc, err := enc.OpenLogs()
		require.NoError(t, err)

		colEnc, err := logsEnc.OpenColumn(logsmd.COLUMN_TYPE_MESSAGE, &columnA.Info)
		require.NoError(t, err)
		for _, page := range columnA.Pages {
			require.NoError(t, colEnc.AppendPage(page))
		}
		require.NoError(t, colEnc.Commit())

		colEnc, err = logsEnc.OpenColumn(logsmd.COLUMN_TYPE_METADATA, &columnB.Info)
		require.NoError(t, err)
		for _, page := range columnB.Pages {
			require.NoError(t, colEnc.AppendPage(page))
		}
		require.NoError(t, colEnc.Commit())

		require.NoError(t, logsEnc.Commit())
		require.NoError(t, enc.Flush())
	})

	t.Run("Decode", func(t *testing.T) {
		dec := encoding.ReaderAtDecoder(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
		sections, err := dec.Sections(context.TODO())
		require.NoError(t, err)
		require.Len(t, sections, 1)
		require.Equal(t, filemd.SECTION_TYPE_LOGS, sections[0].Type)

		logsDec := dec.LogsDecoder()
		columns, err := logsDec.Columns(context.TODO(), sections[0])
		require.NoError(t, err)
		require.Len(t, columns, 2)

		pageSets, err := result.Collect(logsDec.Pages(context.TODO(), columns))
		require.NoError(t, err)
		require.Len(t, pageSets, 2)

		columnAPages, err := result.Collect(logsDec.ReadPages(context.TODO(), pageSets[0]))
		require.NoError(t, err)
		require.Len(t, columnAPages, len(columnA.Pages))

		for i := range columnA.Pages {
			require.Equal(t, columnA.Pages[i].Data, columnAPages[i])
		}

		columnBPages, err := result.Collect(logsDec.ReadPages(context.TODO(), pageSets[1]))
		require.NoError(t, err)
		require.Len(t, columnBPages, len(columnB.Pages))

		for i := range columnB.Pages {
			require.Equal(t, columnB.Pages[i].Data, columnBPages[i])
		}
	})
}
