package pointers

import (
	"bytes"
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

func TestAddingStreams(t *testing.T) {
	type ent struct {
		path             string
		section          int64
		streamIDInObject int64
		streamID         int64
		minTimestamp     time.Time
		maxTimestamp     time.Time
		rowsCount        int64
		uncompressedSize int64
	}

	tt := []ent{
		{path: "foo", section: 0, streamIDInObject: -1, streamID: 1, minTimestamp: time.Unix(9, 0), maxTimestamp: time.Unix(15, 0), rowsCount: 2, uncompressedSize: 30},
		{path: "bar", section: 1, streamIDInObject: -2, streamID: 2, minTimestamp: time.Unix(100, 0), maxTimestamp: time.Unix(101, 0), rowsCount: 2, uncompressedSize: 20},
	}

	tracker := NewBuilder(nil, 1024)
	for _, tc := range tt {
		tracker.RecordStreamRef(tc.path, tc.streamIDInObject, tc.streamID)
		// Observe twice to track both min & max timestamps. The size is 0 to avoid double counting it.
		tracker.ObserveStream(tc.path, tc.section, tc.streamIDInObject, tc.minTimestamp, tc.uncompressedSize)
		tracker.ObserveStream(tc.path, tc.section, tc.streamIDInObject, tc.maxTimestamp, 0)
	}

	buf, err := buildObject(tracker)
	require.NoError(t, err)

	expect := []SectionPointer{
		{
			Path:             "foo",
			Section:          0,
			StreamID:         1,
			StreamIDRef:      -1,
			StartTs:          time.Unix(9, 0),
			EndTs:            time.Unix(15, 0),
			LineCount:        2,
			UncompressedSize: 30,
			// Non stream pieces are set to default values
			ValuesBloomFilter: nil,
			Column:            "",
		},
		{
			Path:             "bar",
			Section:          1,
			StreamID:         2,
			StreamIDRef:      -2,
			StartTs:          time.Unix(100, 0),
			EndTs:            time.Unix(101, 0),
			LineCount:        2,
			UncompressedSize: 20,
			// Non stream pieces are set to default values
			ValuesBloomFilter: nil,
			Column:            "",
		},
	}

	obj, err := dataobj.FromReaderAt(bytes.NewReader(buf), int64(len(buf)))
	require.NoError(t, err)

	var actual []SectionPointer
	for result := range Iter(context.Background(), obj) {
		pointer, err := result.Value()
		require.NoError(t, err)
		actual = append(actual, pointer)
	}

	require.Equal(t, expect, actual)
}

func TestAddingColumnIndexes(t *testing.T) {
	type ent struct {
		path              string
		section           int64
		column            string
		valuesBloomFilter []byte
	}

	tt := []ent{
		{path: "foo", section: 0, column: "testColumn", valuesBloomFilter: []byte{1, 2, 3}},
		{path: "bar", section: 1, column: "testColumn2", valuesBloomFilter: []byte{1, 2, 3, 4}},
	}

	tracker := NewBuilder(nil, 1024)
	for _, tc := range tt {
		tracker.RecordColumnIndex(tc.path, tc.section, tc.column, tc.valuesBloomFilter)
	}

	buf, err := buildObject(tracker)
	require.NoError(t, err)

	expect := []SectionPointer{
		{
			Path:              "foo",
			Section:           0,
			Column:            "testColumn",
			ValuesBloomFilter: []byte{1, 2, 3},
			// Non column pieces are set to default values
			StreamID:         0,
			StartTs:          time.Time{},
			EndTs:            time.Time{},
			LineCount:        0,
			UncompressedSize: 0,
		},
		{
			Path:              "bar",
			Section:           1,
			Column:            "testColumn2",
			ValuesBloomFilter: []byte{1, 2, 3, 4},
			// Non column pieces are set to default values
			StreamID:         0,
			StartTs:          time.Time{},
			EndTs:            time.Time{},
			LineCount:        0,
			UncompressedSize: 0,
		},
	}

	obj, err := dataobj.FromReaderAt(bytes.NewReader(buf), int64(len(buf)))
	require.NoError(t, err)

	var actual []SectionPointer
	for result := range Iter(context.Background(), obj) {
		pointer, err := result.Value()
		require.NoError(t, err)
		actual = append(actual, pointer)
	}

	require.Equal(t, expect, actual)
}

func buildObject(st *Builder) ([]byte, error) {
	var buf bytes.Buffer

	builder := dataobj.NewBuilder()
	if err := builder.Append(st); err != nil {
		return nil, err
	} else if _, err := builder.Flush(&buf); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
