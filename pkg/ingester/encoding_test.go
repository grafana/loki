package ingester

import (
	"testing"
	"time"

	"github.com/grafana/loki/pkg/logproto"
	"github.com/prometheus/prometheus/pkg/labels"
	"github.com/prometheus/prometheus/tsdb/record"
	"github.com/stretchr/testify/require"
)

func Test_Encoding_Series(t *testing.T) {
	record := &WALRecord{
		UserID: "123",
		Series: []record.RefSeries{
			{
				Ref: 456,
				Labels: labels.FromMap(map[string]string{
					"foo":  "bar",
					"bazz": "buzz",
				}),
			},
			{
				Ref: 789,
				Labels: labels.FromMap(map[string]string{
					"abc": "123",
					"def": "456",
				}),
			},
		},
	}

	buf := record.encodeSeries(nil)

	var decoded WALRecord

	err := decodeWALRecord(buf, &decoded)
	require.Nil(t, err)
	require.Equal(t, record, &decoded)
}

func Test_Encoding_Entries(t *testing.T) {
	record := &WALRecord{
		UserID: "123",
		RefEntries: RefEntries{
			Ref: 456,
			Entries: []logproto.Entry{
				{
					Timestamp: time.Unix(1000, 0),
					Line:      "first",
				},
				{
					Timestamp: time.Unix(2000, 0),
					Line:      "second",
				},
			},
		},
	}

	buf := record.encodeEntries(nil)

	var decoded WALRecord

	err := decodeWALRecord(buf, &decoded)
	require.Nil(t, err)
	require.Equal(t, record, &decoded)
}
