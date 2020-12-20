// Copyright (c) The Thanos Authors.
// Licensed under the Apache License 2.0.

package downsample

import (
	"encoding/binary"

	"github.com/pkg/errors"
	"github.com/prometheus/prometheus/tsdb/chunkenc"
)

// ChunkEncAggr is the top level encoding byte for the AggrChunk.
// It picks the highest number possible to prevent future collisions with wrapped encodings.
const ChunkEncAggr = chunkenc.Encoding(0xff)

// AggrChunk is a chunk that is composed of a set of aggregates for the same underlying data.
// Not all aggregates must be present.
type AggrChunk []byte

// EncodeAggrChunk encodes a new aggregate chunk from the array of chunks for each aggregate.
// Each array entry corresponds to the respective AggrType number.
func EncodeAggrChunk(chks [5]chunkenc.Chunk) *AggrChunk {
	var b []byte
	buf := [8]byte{}

	for _, c := range chks {
		// Unset aggregates are marked with a zero length entry.
		if c == nil {
			n := binary.PutUvarint(buf[:], 0)
			b = append(b, buf[:n]...)
			continue
		}
		l := len(c.Bytes())
		n := binary.PutUvarint(buf[:], uint64(l))
		b = append(b, buf[:n]...)
		b = append(b, byte(c.Encoding()))
		b = append(b, c.Bytes()...)
	}
	chk := AggrChunk(b)
	return &chk
}

func (c AggrChunk) Bytes() []byte {
	return []byte(c)
}

func (c AggrChunk) Appender() (chunkenc.Appender, error) {
	return nil, errors.New("not implemented")
}

func (c AggrChunk) Iterator(_ chunkenc.Iterator) chunkenc.Iterator {
	return chunkenc.NewNopIterator()
}

func (c AggrChunk) NumSamples() int {
	x, err := c.Get(AggrCount)
	if err != nil {
		return 0
	}
	return x.NumSamples()
}

// ErrAggrNotExist is returned if a requested aggregation is not present in an AggrChunk.
var ErrAggrNotExist = errors.New("aggregate does not exist")

func (c AggrChunk) Encoding() chunkenc.Encoding {
	return ChunkEncAggr
}

func (c AggrChunk) Compact() {}

// Get returns the sub-chunk for the given aggregate type if it exists.
func (c AggrChunk) Get(t AggrType) (chunkenc.Chunk, error) {
	b := c[:]
	var x []byte

	for i := AggrType(0); i <= t; i++ {
		l, n := binary.Uvarint(b)
		if n < 1 || len(b[n:]) < int(l)+1 {
			return nil, errors.New("invalid size")
		}
		b = b[n:]
		// If length is set to zero explicitly, that means the aggregate is unset.
		if l == 0 {
			if i == t {
				return nil, ErrAggrNotExist
			}
			continue
		}
		x = b[:int(l)+1]
		b = b[int(l)+1:]
	}
	return chunkenc.FromData(chunkenc.Encoding(x[0]), x[1:])
}

// AggrType represents an aggregation type.
type AggrType uint8

// Valid aggregations.
const (
	AggrCount AggrType = iota
	AggrSum
	AggrMin
	AggrMax
	AggrCounter
)

func (t AggrType) String() string {
	switch t {
	case AggrCount:
		return "count"
	case AggrSum:
		return "sum"
	case AggrMin:
		return "min"
	case AggrMax:
		return "max"
	case AggrCounter:
		return "counter"
	}
	return "<unknown>"
}
