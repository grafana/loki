package syntax

import (
	"encoding/binary"

	"github.com/prometheus/prometheus/model/labels"
)

// Binary encoding of the LineFilter
// integer is varint encoded
// strings are variable-length encoded
//
// +---------+--------------+-------------+
// | Ty      | Match        | Op          |
// +---------+--------------+-------------+
// | value   | len  | value | len | value |
// +---------+--------------+-------------+

func (lf LineFilter) Equal(o LineFilter) bool {
	return lf.Ty == o.Ty &&
		lf.Match == o.Match &&
		lf.Op == o.Op
}

func (lf LineFilter) Size() int {
	return lenUint64(uint64(lf.Ty)) +
		lenUint64(uint64(len(lf.Match))) +
		len(lf.Match) +
		lenUint64(uint64(len(lf.Op))) +
		len(lf.Op)
}

func (lf LineFilter) MarshalTo(b []byte) (int, error) {
	n := 0
	// lf.Ty
	n += binary.PutUvarint(b[n:], uint64(lf.Ty))
	// lf.Match
	n += binary.PutUvarint(b[n:], uint64(len(lf.Match)))
	n += copy(b[n:], lf.Match)
	// lf.Op
	n += binary.PutUvarint(b[n:], uint64(len(lf.Op)))
	n += copy(b[n:], lf.Op)
	return n, nil
}

func (lf *LineFilter) Unmarshal(b []byte) error {
	i := 0
	// lf.Ty
	v, n := binary.Uvarint(b)
	lf.Ty = labels.MatchType(v)
	i += n
	// lf.Match
	v, n = binary.Uvarint(b[i:])
	i += n
	lf.Match = string(b[i : i+int(v)])
	i += int(v)
	// lf.Op
	v, n = binary.Uvarint(b[i:])
	i += n
	lf.Op = string(b[i : i+int(v)])
	i += int(v) // nolint:ineffassign
	return nil
}

// utility copied from implementation of binary.PutUvarint()
func lenUint64(x uint64) int {
	i := 0
	for x >= 0x80 {
		x >>= 7
		i++
	}
	return i + 1
}
