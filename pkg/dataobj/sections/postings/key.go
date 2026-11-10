package postings

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
)

// Key identifies one posting: the rows of a postings section that share a
// kind, a column name and a label value. Rows sort by Key first, so the rows of
// one posting are adjacent.
type Key struct {
	Kind       PostingKind
	ColumnName string
	LabelValue string // Empty for KindBloom.
}

// Key returns the posting key of r.
func (r Row) Key() Key {
	return Key{Kind: r.Kind, ColumnName: r.ColumnName, LabelValue: r.LabelValue}
}

// Compare reports whether k sorts before (<0), after (>0), or equal to (0)
// other. The order is the prefix of [CompareRows].
func (k Key) Compare(other Key) int {
	return cmp.Or(
		cmp.Compare(k.Kind, other.Kind),
		cmp.Compare(k.ColumnName, other.ColumnName),
		cmp.Compare(k.LabelValue, other.LabelValue),
	)
}

var errInvalidKeyEncoding = errors.New("invalid posting key encoding")

// appendEncoding appends the encoding of k to buf. The encoding is part of the
// storage format. It is:
//
//   - the kind as one byte;
//   - the length of the column name as a uvarint, then the column name;
//   - the length of the label value as a uvarint, then the label value.
//
// The byte order of two encodings is not the order of their keys. Compare
// decoded keys with [Key.Compare].
//
// The kind of k must fit in one byte. [NewKeyRange] checks it.
func (k Key) appendEncoding(buf []byte) []byte {
	buf = append(buf, byte(k.Kind))
	buf = binary.AppendUvarint(buf, uint64(len(k.ColumnName)))
	buf = append(buf, k.ColumnName...)
	buf = binary.AppendUvarint(buf, uint64(len(k.LabelValue)))
	return append(buf, k.LabelValue...)
}

// decodeKey decodes a Key from the output of appendEncoding. The Key does not
// borrow memory from data.
func decodeKey(data []byte) (Key, error) {
	if len(data) == 0 {
		return Key{}, fmt.Errorf("%w: no kind", errInvalidKeyEncoding)
	}
	key := Key{Kind: PostingKind(data[0])}
	rest := data[1:]

	columnName, rest, err := decodeString(rest)
	if err != nil {
		return Key{}, fmt.Errorf("%w: column name: %w", errInvalidKeyEncoding, err)
	}
	labelValue, rest, err := decodeString(rest)
	if err != nil {
		return Key{}, fmt.Errorf("%w: label value: %w", errInvalidKeyEncoding, err)
	}
	if len(rest) > 0 {
		return Key{}, fmt.Errorf("%w: %d bytes after the label value", errInvalidKeyEncoding, len(rest))
	}

	key.ColumnName, key.LabelValue = columnName, labelValue
	return key, nil
}

// decodeString decodes a uvarint length and the string that follows it from
// data. It returns the string and the bytes after it.
func decodeString(data []byte) (string, []byte, error) {
	length, n := binary.Uvarint(data)
	if n <= 0 {
		return "", nil, errors.New("invalid length")
	}
	data = data[n:]
	if length > uint64(len(data)) {
		return "", nil, fmt.Errorf("length %d is longer than the %d remaining bytes", length, len(data))
	}
	return string(data[:length]), data[length:], nil
}

// KeyRange is an inclusive range of posting keys. Min never sorts after Max.
// The kinds of both keys fit in one byte, and their column names are not
// empty.
//
// The zero value means no bounds: the range is unknown and can hold any key.
// [NewKeyRange] never returns the zero value, because no posting has an empty
// column name.
type KeyRange struct {
	min, max Key
}

// NewKeyRange returns the range from minKey to maxKey, both inclusive. It
// returns an error if minKey sorts after maxKey, if the kind of a key does not
// fit in one byte, or if a key has an empty column name.
func NewKeyRange(minKey, maxKey Key) (KeyRange, error) {
	for _, key := range []Key{minKey, maxKey} {
		if key.Kind < 0 || key.Kind > math.MaxUint8 {
			return KeyRange{}, fmt.Errorf("posting key range: kind %d does not fit in one byte", key.Kind)
		}
		if key.ColumnName == "" {
			return KeyRange{}, errors.New("posting key range: empty column name")
		}
	}
	if minKey.Compare(maxKey) > 0 {
		return KeyRange{}, fmt.Errorf("posting key range: min %v sorts after max %v", minKey, maxKey)
	}
	return KeyRange{min: minKey, max: maxKey}, nil
}

// DecodeKeyRange returns the range whose bounds have the encodings minEncoded
// and maxEncoded, which [KeyRange.Encode] returned. It returns an error if an
// encoding is not valid, or if the min bound sorts after the max bound. The
// range does not borrow memory from the encodings.
func DecodeKeyRange(minEncoded, maxEncoded []byte) (KeyRange, error) {
	minKey, err := decodeKey(minEncoded)
	if err != nil {
		return KeyRange{}, fmt.Errorf("decoding min bound: %w", err)
	}
	maxKey, err := decodeKey(maxEncoded)
	if err != nil {
		return KeyRange{}, fmt.Errorf("decoding max bound: %w", err)
	}
	return NewKeyRange(minKey, maxKey)
}

// IsZero reports whether r is the zero KeyRange, which means no bounds.
func (r KeyRange) IsZero() bool { return r == KeyRange{} }

// Min returns the smallest key of r.
func (r KeyRange) Min() Key { return r.min }

// Max returns the largest key of r.
func (r KeyRange) Max() Key { return r.max }

// Encode returns the encodings of the min and max bounds of r. r must not be
// the zero KeyRange.
func (r KeyRange) Encode() (minEncoded, maxEncoded []byte) {
	return r.min.appendEncoding(nil), r.max.appendEncoding(nil)
}
