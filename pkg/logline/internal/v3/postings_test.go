package v3

import (
	"testing"

	"github.com/RoaringBitmap/roaring"
	"github.com/stretchr/testify/require"
)

func TestDeltaVarIntRoundTrip(t *testing.T) {
	cases := []struct {
		name   string
		docIDs []uint32
	}{
		{"empty", nil},
		{"single", []uint32{42}},
		{"small", []uint32{1, 5, 100, 200, 300}},
		{"large_gaps", []uint32{0, 100000, 200000}},
		{"consecutive", []uint32{10, 11, 12, 13, 14}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			bm := roaring.New()
			bm.AddMany(tc.docIDs)
			payload := appendDeltaVarInt(nil, bm.ToArray())
			got, err := decodeDeltaVarInt(payload)
			require.NoError(t, err)
			require.Equal(t, tc.docIDs, got)
		})
	}
}
