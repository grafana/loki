package parser

// Reader is the interface that wraps the Read method.
// Its implementations are intended to be used with Ragel parsers or scanners.
type Reader interface {
	Read() (res []byte, err error)
	State() *State
}

// Seeker is the interface that wraps the Seek method.
// Its implementations are intended to be used with Ragel parsers or scanners.
type Seeker interface {
	Seek(until byte, backward bool) (n int, err error)
}
