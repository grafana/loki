package encoding_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/result"
)

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
		metadata, err := dec.Metadata(context.TODO())
		require.NoError(t, err)
		require.Len(t, metadata.Sections, 1)

		typ, err := encoding.GetSectionType(metadata, metadata.Sections[0])
		require.NoError(t, err)
		require.Equal(t, encoding.SectionTypeLogs, typ)

		logsDec := dec.LogsDecoder(metadata, metadata.Sections[0])
		columns, err := logsDec.Columns(context.TODO())
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
