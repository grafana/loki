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
		_, err := NewKeyRange(Key{Kind: -1, ColumnName: "x"}, high)
		require.Error(t, err)
	})

	t.Run("returns an error when a kind does not fit in one byte", func(t *testing.T) {
		_, err := NewKeyRange(low, Key{Kind: 256, ColumnName: "x"})
		require.Error(t, err)
	})

	t.Run("returns an error when a key has an empty column name", func(t *testing.T) {
		_, err := NewKeyRange(Key{Kind: KindBloom}, high)
		require.Error(t, err)
	})
}

func TestKeyRange_IsZero(t *testing.T) {
	t.Run("returns true for the zero KeyRange", func(t *testing.T) {
		require.True(t, KeyRange{}.IsZero())
	})

	t.Run("returns false for every range that NewKeyRange returns", func(t *testing.T) {
		low := Key{Kind: KindBloom, ColumnName: "a"}
		r, err := NewKeyRange(low, low)
		require.NoError(t, err)
		require.False(t, r.IsZero())
	})
}

func TestDecodeKeyRange(t *testing.T) {
	var keys []Key
	for _, key := range keyOrderCases {
		if key.ColumnName != "" {
			keys = append(keys, key)
		}
	}

	t.Run("returns the same range for the encoding of every range", func(t *testing.T) {
		for i, low := range keys {
			for _, high := range keys[i:] {
				want, err := NewKeyRange(low, high)
				require.NoError(t, err)

				got, err := DecodeKeyRange(want.Encode())
				require.NoError(t, err)
				require.Equal(t, want, got)
			}
		}
	})

	t.Run("returns an error when a bound has an empty column name", func(t *testing.T) {
		_, err := DecodeKeyRange([]byte{byte(KindLabel), 0, 0}, []byte{byte(KindLabel), 1, 'a', 0})
		require.Error(t, err)
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
