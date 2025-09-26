package dataset_test

import (
	"crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/datasetmd"
)

func binaryValue(n int) dataset.Value {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return dataset.BinaryValue(b)
}

func TestColumnBuilder_Append(t *testing.T) {
	tests := []struct {
		name            string
		pageSizeHint    int
		pageMaxRowCount int
		rowsBefore      []dataset.Value
		rowIndex        int
		value           dataset.Value
		wantErr         error
		expPages        int
	}{
		{
			name:            "row exceeds target size hint",
			pageSizeHint:    512,
			pageMaxRowCount: 0,
			rowsBefore:      []dataset.Value{binaryValue(128)},
			rowIndex:        1,
			value:           binaryValue(512),
			wantErr:         nil,
			expPages:        2,
		},
		{
			name:            "row exceeds max row count",
			pageSizeHint:    0,
			pageMaxRowCount: 2,
			rowsBefore:      []dataset.Value{binaryValue(128), binaryValue(128)},
			rowIndex:        2,
			value:           binaryValue(128),
			wantErr:         nil,
			expPages:        2,
		},
		{
			name:            "first not NULL row exceeds target size hint",
			pageSizeHint:    2,
			pageMaxRowCount: 0,
			rowsBefore:      nil,
			rowIndex:        9,
			value:           binaryValue(1),
			wantErr:         nil,
			expPages:        1, // only 1 page, because 9 NULL rows do not count anything towards estimated size and therefore no page is explicitly flushed
		},
		{
			name:            "first not NULL row exceeds max row count",
			pageSizeHint:    0,
			pageMaxRowCount: 1,
			rowsBefore:      nil,
			rowIndex:        9,
			value:           binaryValue(1024),
			wantErr:         nil,
			expPages:        2,
		},
		{
			name:            "backfilling exceeds max row count",
			pageSizeHint:    0,
			pageMaxRowCount: 5,
			rowsBefore:      []dataset.Value{binaryValue(128), binaryValue(128)},
			rowIndex:        16,
			value:           binaryValue(128),
			wantErr:         nil,
			expPages:        2,
		},
		{
			name:            "backfilling exactly matches max row count",
			pageSizeHint:    0,
			pageMaxRowCount: 5,
			rowsBefore:      []dataset.Value{binaryValue(128), binaryValue(128), binaryValue(128), binaryValue(128)},
			rowIndex:        9,
			value:           binaryValue(128),
			wantErr:         nil,
			expPages:        3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			opts := dataset.BuilderOptions{
				PageSizeHint:    tt.pageSizeHint,
				PageMaxRowCount: tt.pageMaxRowCount,
				Type:            dataset.ColumnType{Physical: datasetmd.PHYSICAL_TYPE_BINARY, Logical: "data"},
				Compression:     datasetmd.COMPRESSION_TYPE_ZSTD,
				Encoding:        datasetmd.ENCODING_TYPE_PLAIN,
			}

			cb, err := dataset.NewColumnBuilder("test", opts)
			require.NoError(t, err)

			for i, value := range tt.rowsBefore {
				err = cb.Append(i, value)
				require.NoError(t, err, "setup of exisiting rows failed")
			}

			err = cb.Append(tt.rowIndex, tt.value)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
			}

			col, err := cb.Flush()
			require.NoError(t, err)
			require.Equal(t, tt.expPages, len(col.Pages))
		})
	}
}
