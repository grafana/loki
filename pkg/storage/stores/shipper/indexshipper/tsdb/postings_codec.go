package tsdb

import (
	"bytes"

	"github.com/golang/snappy"
	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/storage"
	promEncoding "github.com/prometheus/prometheus/tsdb/encoding"

	"github.com/grafana/loki/pkg/storage/stores/shipper/indexshipper/tsdb/index"
	"github.com/grafana/loki/pkg/util/encoding"
)

type codec string

const (
	codecHeaderSnappy codec = "dvs" // As in "diff+varint+snappy".
)

// isDiffVarintSnappyEncodedPostings returns true, if input looks like it has been encoded by diff+varint+snappy codec.
func isDiffVarintSnappyEncodedPostings(input []byte) bool {
	return bytes.HasPrefix(input, []byte(codecHeaderSnappy))
}

func diffVarintSnappyDecode(input []byte) (index.Postings, error) {
	if !isDiffVarintSnappyEncodedPostings(input) {
		return nil, errors.New(string(codecHeaderSnappy) + " header not found")
	}

	offset := len(codecHeaderSnappy)

	raw, err := snappy.Decode(nil, input[offset:])
	if err != nil {
		return nil, errors.Wrap(err, "snappy decode")
	}

	return newDiffVarintPostings(raw), nil
}

func newDiffVarintPostings(input []byte) *diffVarintPostings {
	return &diffVarintPostings{buf: &promEncoding.Decbuf{B: input}}
}

// diffVarintPostings is an implementation of index.Postings based on diff+varint encoded data.
type diffVarintPostings struct {
	buf *promEncoding.Decbuf
	cur storage.SeriesRef
}

func (it *diffVarintPostings) At() storage.SeriesRef {
	return it.cur
}

func (it *diffVarintPostings) Next() bool {
	if it.buf.Err() != nil || it.buf.Len() == 0 {
		return false
	}

	val := it.buf.Uvarint64()
	if it.buf.Err() != nil {
		return false
	}

	it.cur = it.cur + storage.SeriesRef(val)
	return true
}

func (it *diffVarintPostings) Seek(x storage.SeriesRef) bool {
	if it.cur >= x {
		return true
	}

	// We cannot do any search due to how values are stored,
	// so we simply advance until we find the right value.
	for it.Next() {
		if it.At() >= x {
			return true
		}
	}

	return false
}

func (it *diffVarintPostings) Err() error {
	return it.buf.Err()
}

// diffVarintSnappyEncode encodes postings into diff+varint representation,
// and applies snappy compression on the result.
// Returned byte slice starts with codecHeaderSnappy header.
// Length argument is expected number of postings, used for preallocating buffer.
func diffVarintSnappyEncode(p index.Postings, length int) ([]byte, error) {
	buf, err := diffVarintEncodeNoHeader(p, length)
	if err != nil {
		return nil, err
	}

	// Make result buffer large enough to hold our header and compressed block.
	result := make([]byte, len(codecHeaderSnappy)+snappy.MaxEncodedLen(len(buf)))
	copy(result, codecHeaderSnappy)

	compressed := snappy.Encode(result[len(codecHeaderSnappy):], buf)

	// Slice result buffer based on compressed size.
	result = result[:len(codecHeaderSnappy)+len(compressed)]
	return result, nil
}

// diffVarintEncodeNoHeader encodes postings into diff+varint representation.
// It doesn't add any header to the output bytes.
func diffVarintEncodeNoHeader(p index.Postings, length int) ([]byte, error) {
	buf := encoding.Encbuf{}

	// This encoding uses around ~1 bytes per posting, but let's use
	// conservative 1.25 bytes per posting to avoid extra allocations.
	if length > 0 {
		buf.B = make([]byte, 0, 5*length/4)
	}

	prev := storage.SeriesRef(0)
	for p.Next() {
		v := p.At()
		if v < prev {
			return nil, errors.Errorf("postings entries must be in increasing order, current: %d, previous: %d", v, prev)
		}

		// This is the 'diff' part -- compute difference from previous value.
		buf.PutUvarint64(uint64(v - prev))
		prev = v
	}
	if p.Err() != nil {
		return nil, p.Err()
	}

	return buf.B, nil
}
