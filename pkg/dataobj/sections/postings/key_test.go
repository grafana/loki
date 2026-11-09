package postings

import (
	"testing"

	"github.com/stretchr/testify/require"
)

var keyOrderCases = []Key{
	{Kind: KindBloom, ColumnName: "", LabelValue: ""},
	{Kind: KindBloom, ColumnName: "trace_id"},
	{Kind: KindLabel, ColumnName: ""},
	{Kind: KindLabel, ColumnName: "", LabelValue: "x"},
	{Kind: KindLabel, ColumnName: "a"},
	{Kind: KindLabel, ColumnName: "a", LabelValue: "\x00"},
	{Kind: KindLabel, ColumnName: "a", LabelValue: "b"},
	{Kind: KindLabel, ColumnName: "a\x00"},
	{Kind: KindLabel, ColumnName: "ab"},
	{Kind: KindLabel, ColumnName: "app", LabelValue: "api"},
	{Kind: KindLabel, ColumnName: "app", LabelValue: "web"},
	{Kind: KindLabel, ColumnName: "\xff"},
}

func TestKey_Compare(t *testing.T) {
	t.Run("orders keys the same as CompareRows orders rows that differ only in their keys", func(t *testing.T) {
		for i, a := range keyOrderCases {
			for j, b := range keyOrderCases {
				rowA := Row{Kind: a.Kind, ColumnName: a.ColumnName, LabelValue: a.LabelValue}
				rowB := Row{Kind: b.Kind, ColumnName: b.ColumnName, LabelValue: b.LabelValue}
				require.Equal(t, CompareRows(rowA, rowB), a.Compare(b), "keys %d and %d", i, j)
			}
		}
	})

	t.Run("orders rows with different keys by key before the other fields", func(t *testing.T) {
		a := Row{Kind: KindLabel, ColumnName: "app", LabelValue: "api", MinTimestamp: 50}
		b := Row{Kind: KindLabel, ColumnName: "app", LabelValue: "web", MinTimestamp: 10}
		require.Negative(t, a.Key().Compare(b.Key()))
		require.Negative(t, CompareRows(a, b))
	})
}

func TestNewKeyRange(t *testing.T) {
	low := Key{Kind: KindLabel, ColumnName: "app", LabelValue: "api"}
	high := Key{Kind: KindLabel, ColumnName: "env", LabelValue: "prod"}

	t.Run("returns the range when min sorts before max", func(t *testing.T) {
		r, err := NewKeyRange(low, high)
		require.NoError(t, err)
		require.Equal(t, low, r.Min())
		require.Equal(t, high, r.Max())
	})

	t.Run("returns the range when min equals max", func(t *testing.T) {
		r, err := NewKeyRange(low, low)
		require.NoError(t, err)
		require.Equal(t, low, r.Min())
		require.Equal(t, low, r.Max())
	})

	t.Run("returns an error when min sorts after max", func(t *testing.T) {
		_, err := NewKeyRange(high, low)
		require.Error(t, err)
	})

	t.Run("returns an error when a kind is negative", func(t *testing.T) {
		_, err := NewKeyRange(Key{Kind: -1}, high)
		require.Error(t, err)
	})

	t.Run("returns an error when a kind does not fit in one byte", func(t *testing.T) {
		_, err := NewKeyRange(low, Key{Kind: 256})
		require.Error(t, err)
	})
}

func TestDecodeKeyRange(t *testing.T) {
	t.Run("returns the same range for the encoding of every range", func(t *testing.T) {
		for i, low := range keyOrderCases {
			for _, high := range keyOrderCases[i:] {
				want, err := NewKeyRange(low, high)
				require.NoError(t, err)

				got, err := DecodeKeyRange(want.Encode())
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		}
	})

	t.Run("returns an error when the min bound sorts after the max bound", func(t *testing.T) {
		r, err := NewKeyRange(Key{Kind: KindLabel, ColumnName: "a"}, Key{Kind: KindLabel, ColumnName: "b"})
		require.NoError(t, err)
		minEncoded, maxEncoded := r.Encode()

		_, err = DecodeKeyRange(maxEncoded, minEncoded)
		require.Error(t, err)
	})

	r, err := NewKeyRange(Key{Kind: KindLabel, ColumnName: "app", LabelValue: "api"}, Key{Kind: KindLabel, ColumnName: "env"})
	require.NoError(t, err)
	valid, maxEncoded := r.Encode()
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{name: "returns an error when the encoding is empty", data: nil},
		{name: "returns an error when the encoding has no column name length", data: valid[:1]},
		{name: "returns an error when the column name is shorter than its length", data: valid[:3]},
		{name: "returns an error when the encoding has no label value length", data: valid[:5]},
		{name: "returns an error when the label value is shorter than its length", data: valid[:len(valid)-1]},
		{name: "returns an error when bytes follow the label value", data: append(append([]byte{}, valid...), 'x')},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := DecodeKeyRange(tc.data, maxEncoded)
			require.ErrorIs(t, err, errInvalidKeyEncoding)
		})
	}
}
