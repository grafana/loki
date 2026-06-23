package postings

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRow_StreamIDs(t *testing.T) {
	tests := []struct {
		name   string
		bitmap []byte
		want   []int64
	}{
		{"empty", nil, nil},
		{"all-zero", []byte{0x00, 0x00}, nil},
		{"first-bit", []byte{0x01}, []int64{0}},
		{"low-byte", []byte{0b0000_1010}, []int64{1, 3}},
		{"second-byte high bit", []byte{0x00, 0b1000_0000}, []int64{15}},
		{"multi-byte", []byte{0b0000_0001, 0b0000_0001}, []int64{0, 8}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := Row{StreamIDBitmap: tc.bitmap}
			got := slices.Collect(r.StreamIDs())
			require.Equal(t, tc.want, got)
		})
	}
}
