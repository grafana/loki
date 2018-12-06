package encoding

import (
	"fmt"
	"strconv"
)

// Encoding defines which encoding we are using, delta, doubledelta, or varbit
type Encoding byte

// DefaultEncoding can be changed via a flag.
var DefaultEncoding = DoubleDelta

// String implements flag.Value.
func (e Encoding) String() string {
	return fmt.Sprintf("%d", e)
}

const (
	// Delta encoding
	Delta Encoding = iota
	// DoubleDelta encoding
	DoubleDelta
	// Varbit encoding
	Varbit
	// Bigchunk encoding
	Bigchunk
)

type encoding struct {
	Name string
	New  func() Chunk
}

var encodings = map[Encoding]encoding{
	Delta: {
		Name: "Delta",
		New: func() Chunk {
			return newDeltaEncodedChunk(d1, d0, true, ChunkLen)
		},
	},
	DoubleDelta: {
		Name: "DoubleDelta",
		New: func() Chunk {
			return newDoubleDeltaEncodedChunk(d1, d0, true, ChunkLen)
		},
	},
	Varbit: {
		Name: "Varbit",
		New: func() Chunk {
			return newVarbitChunk(varbitZeroEncoding)
		},
	},
	Bigchunk: {
		Name: "Bigchunk",
		New: func() Chunk {
			return newBigchunk()
		},
	},
}

// Set implements flag.Value.
func (e *Encoding) Set(s string) error {
	i, err := strconv.Atoi(s)
	if err != nil {
		return err
	}

	_, ok := encodings[Encoding(i)]
	if !ok {
		return fmt.Errorf("invalid chunk encoding: %s", s)
	}

	*e = Encoding(i)
	return nil
}

// New creates a new chunk according to the encoding set by the
// DefaultEncoding flag.
func New() Chunk {
	chunk, err := NewForEncoding(DefaultEncoding)
	if err != nil {
		panic(err)
	}
	return chunk
}

// NewForEncoding allows configuring what chunk type you want
func NewForEncoding(encoding Encoding) (Chunk, error) {
	enc, ok := encodings[encoding]
	if !ok {
		return nil, fmt.Errorf("unknown chunk encoding: %v", encoding)
	}

	return enc.New(), nil
}

// MustRegisterEncoding add a new chunk encoding.  There is no locking, so this
// must be called in init().
func MustRegisterEncoding(enc Encoding, name string, new func() Chunk) {
	_, ok := encodings[enc]
	if ok {
		panic("double register encoding")
	}

	encodings[enc] = encoding{
		Name: name,
		New:  new,
	}
}
