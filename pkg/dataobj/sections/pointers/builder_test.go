package pointers

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/grafana/loki/v3/pkg/dataobj"
)

func TestAddingStreams(t *testing.T) {
	type ent struct {
		path             string
		section          int64
		pointerKind      PointerKind
		streamIDInObject int64
		streamID         int64
		minTimestamp     time.Time
		maxTimestamp     time.Time
		rowsCount        int64
		uncompressedSize int64
	}

	tt := []ent{
		{path: "foo", section: 0, pointerKind: PointerKindStreamIndex, streamIDInObject: -1, streamID: 1, minTimestamp: time.Unix(9, 0), maxTimestamp: time.Unix(15, 0), rowsCount: 2, uncompressedSize: 30},
		{path: "bar", section: 1, pointerKind: PointerKindStreamIndex, streamIDInObject: -2, streamID: 2, minTimestamp: time.Unix(100, 0), maxTimestamp: time.Unix(101, 0), rowsCount: 2, uncompressedSize: 20},
	}

	tracker := NewBuilder(nil, 1024)
	for _, tc := range tt {
		tracker.ObserveStream(tc.path, tc.section, tc.streamIDInObject, tc.streamID, tc.minTimestamp, tc.uncompressedSize)
		// We Observe twice to track the max timestamp too. But we don't want to count the size twice.
		tracker.ObserveStream(tc.path, tc.section, tc.streamIDInObject, tc.streamID, tc.maxTimestamp, 0)
	}

	obj, closer, err := buildObject(tracker)
	require.NoError(t, err)
	defer closer.Close()

	expect := []SectionPointer{
		{
			Path:             "foo",
			Section:          0,
			PointerKind:      PointerKindStreamIndex,
			StreamID:         1,
			StreamIDRef:      -1,
			StartTs:          time.Unix(9, 0),
			EndTs:            time.Unix(15, 0),
			LineCount:        2,
			UncompressedSize: 30,
			// Non stream pieces are set to default values
			ValuesBloomFilter: nil,
			ColumnName:        "",
			ColumnIndex:       0,
		},
		{
			Path:             "bar",
			Section:          1,
			PointerKind:      PointerKindStreamIndex,
			StreamID:         2,
			StreamIDRef:      -2,
			StartTs:          time.Unix(100, 0),
			EndTs:            time.Unix(101, 0),
			LineCount:        2,
			UncompressedSize: 20,
			// Non stream pieces are set to default values
			ValuesBloomFilter: nil,
			ColumnName:        "",
			ColumnIndex:       0,
		},
	}

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
		pointerKind       PointerKind
		columnName        string
		columnIndex       int64
		valuesBloomFilter []byte
	}

	tt := []ent{
		{path: "foo", section: 0, pointerKind: PointerKindColumnIndex, columnName: "testColumn", columnIndex: 0, valuesBloomFilter: []byte{1, 2, 3}},
		{path: "bar", section: 1, pointerKind: PointerKindColumnIndex, columnName: "testColumn2", columnIndex: 1, valuesBloomFilter: []byte{1, 2, 3, 4}},
	}

	tracker := NewBuilder(nil, 1024)
	for _, tc := range tt {
		tracker.RecordColumnIndex(tc.path, tc.section, tc.columnName, tc.columnIndex, tc.valuesBloomFilter)
	}

	obj, closer, err := buildObject(tracker)
	require.NoError(t, err)
	defer closer.Close()

	expect := []SectionPointer{
		{
			Path:              "foo",
			Section:           0,
			PointerKind:       PointerKindColumnIndex,
			ColumnName:        "testColumn",
			ColumnIndex:       0,
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
			PointerKind:       PointerKindColumnIndex,
			ColumnName:        "testColumn2",
			ColumnIndex:       1,
			ValuesBloomFilter: []byte{1, 2, 3, 4},
			// Non column pieces are set to default values
			StreamID:         0,
			StartTs:          time.Time{},
			EndTs:            time.Time{},
			LineCount:        0,
			UncompressedSize: 0,
		},
	}

	var actual []SectionPointer
	for result := range Iter(context.Background(), obj) {
		pointer, err := result.Value()
		require.NoError(t, err)
		actual = append(actual, pointer)
	}

	require.Equal(t, expect, actual)
}

func buildObject(st *Builder) (*dataobj.Object, io.Closer, error) {
	builder := dataobj.NewBuilder()
	if err := builder.Append(st); err != nil {
		return nil, nil, err
	}
	return builder.Flush()
}
