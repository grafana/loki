package chunk

import (
	"fmt"
	"strconv"
)

// Encoding defines which encoding we are using, delta, doubledelta, or varbit
type Encoding byte

// Config configures the behaviour of chunk encoding
type Config struct{}

var (
	// DefaultEncoding exported for use in unit tests elsewhere
	DefaultEncoding = Bigchunk
)

// String implements flag.Value.
func (e Encoding) String() string {
	if known, found := encodings[e]; found {
		return known.Name
	}
	return fmt.Sprintf("%d", e)
}

const (
	// Big chunk encoding.
	Bigchunk Encoding = iota
)

type encoding struct {
	Name string
	New  func() Data
}

var encodings = map[Encoding]encoding{
	Bigchunk: {
		Name: "Bigchunk",
		New: func() Data {
			return newBigchunk()
		},
	},
}

// Set implements flag.Value.
func (e *Encoding) Set(s string) error {
	// First see if the name was given
	for k, v := range encodings {
		if s == v.Name {
			*e = k
			return nil
		}
	}
	// Otherwise, accept a number
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
func New() Data {
	chunk, err := NewForEncoding(DefaultEncoding)
	if err != nil {
		panic(err)
	}
	return chunk
}

// NewForEncoding allows configuring what chunk type you want
func NewForEncoding(encoding Encoding) (Data, error) {
	enc, ok := encodings[encoding]
	if !ok {
		return nil, fmt.Errorf("unknown chunk encoding: %v", encoding)
	}

	return enc.New(), nil
}

// MustRegisterEncoding add a new chunk encoding.  There is no locking, so this
// must be called in init().
func MustRegisterEncoding(enc Encoding, name string, f func() Data) {
	_, ok := encodings[enc]
	if ok {
		panic("double register encoding")
	}

	encodings[enc] = encoding{
		Name: name,
		New:  f,
	}
}
