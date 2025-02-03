package encoding_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj/internal/dataset"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/encoding"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/logsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/metadata/streamsmd"
	"github.com/grafana/loki/v3/pkg/dataobj/internal/streamio"
)

func Test_Encoders(t *testing.T) {
	newEncoders := getEncoders(t)

	for _, newEncoder := range newEncoders {
		t.Run(fmt.Sprintf("%T/Only permit one call to Commit", newEncoder), func(t *testing.T) {
			encoder := newEncoder()

			require.NoError(t, encoder.Commit())
			require.ErrorIs(t, encoder.Commit(), encoding.ErrClosed)
		})

		t.Run(fmt.Sprintf("%T/Commit must fail after discard", newEncoder), func(t *testing.T) {
			encoder := newEncoder()

			require.NoError(t, encoder.Discard())
			require.ErrorIs(t, encoder.Commit(), encoding.ErrClosed)
		})
	}
}

type encoder interface {
	Commit() error
	Discard() error
}

type newEncoder func() encoder

func getEncoders(t *testing.T) []newEncoder {
	t.Helper()

	return []newEncoder{
		func() encoder {
			enc := encoding.NewEncoder(streamio.Discard)
			streamsEnc, err := enc.OpenStreams()
			require.NoError(t, err)
			return streamsEnc
		},

		func() encoder {
			enc := encoding.NewEncoder(streamio.Discard)
			streamsEnc, err := enc.OpenStreams()
			require.NoError(t, err)
			columnEnc, err := streamsEnc.OpenColumn(streamsmd.COLUMN_TYPE_LABEL, &dataset.ColumnInfo{})
			require.NoError(t, err)
			return columnEnc
		},

		func() encoder {
			enc := encoding.NewEncoder(streamio.Discard)
			logsEnc, err := enc.OpenLogs()
			require.NoError(t, err)
			return logsEnc
		},

		func() encoder {
			enc := encoding.NewEncoder(streamio.Discard)
			logsEnc, err := enc.OpenLogs()
			require.NoError(t, err)
			columnEnc, err := logsEnc.OpenColumn(logsmd.COLUMN_TYPE_MESSAGE, &dataset.ColumnInfo{})
			require.NoError(t, err)
			return columnEnc
		},
	}
}
