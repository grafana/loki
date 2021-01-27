package parser

import (
	"bufio"
	ragel "github.com/leodido/ragel-machinery"
	"io"
)

// ArbitraryReader returns a Reader that reads from r
// but stops when it finds a delimiter.
// The underlying implementation is a *DelimitedReader.
func ArbitraryReader(r io.Reader, delim byte) *DelimitedReader {
	return &DelimitedReader{
		delim:  delim,
		reader: bufio.NewReader(r),
		parsingState: &parsingState{
			data: []byte{},
			p:    0,
			pe:   0,
			eof:  -1,
		},
	}
}

// DelimitedReader reads arbitrarily sized bytes slices until a delimiter is found.
// It depends on and it keeps track of Ragel's state variables.
type DelimitedReader struct {
	*parsingState

	delim  byte
	reader *bufio.Reader
}

// State returns a pointer to the current State.
func (r DelimitedReader) State() *State {
	return (*State)(r.parsingState)
}

// Read reads a chunk of bytes until it finds a delimiter.
//
// It always works on the current boundaries of the data,
// and updates them accordingly.
// It returns the chunk of bytes read and, eventually, an error.
// When delim is not found it returns an io.ErrUnexpectedEOF.
func (r *DelimitedReader) Read() (line []byte, err error) {
	p := r.p

	// Process only the data still to read when P is greater than the half of the data
	// data = a b c d e f g h i l m n
	// vars = - - - - - - - p - pe- -
	if p > len(r.data)/2 {
		copy(r.data, r.data[p:len(r.data)])
		r.p = 0
		r.pe = r.pe - p
		// data = h i l m n f g h i l m n
		// vars = p - pe- - - - - - - - -
		r.data = r.data[0 : len(r.data)-p]
		// data = h i l m n
		// vars = p - pe- -
	}

	// Read until the first occurrence of the delimiter
	line, err = r.reader.ReadBytes(r.delim)

	// Storing the data up to and including the delimiter
	r.data = append(r.data, line...)
	// Update the position of end
	r.pe = len(r.data)
	if err == io.EOF {
		if len(line) != 0 {
			err = io.ErrUnexpectedEOF
		}
		// Update the position of EOF
		r.eof = r.pe
	}
	if err != nil {
		err = ragel.NewReadingError(err.Error())
	}

	return line, err
}

// Seek look for the first instance of until.
//
// It always works on the current boundaries of the data.
// When it finds what it looks for it returns the number of bytes sought before to find it.
// Otherwise will also return an error.
// Search is only backwards at the moment,
// from the right boundary (end) to the left one (start position).
// It returns the number of bytes read to find the first occurrence of the until byte.
// It sets the right boundary (end) of the data to the character after the until byte,
// so the user can eventually start again from here.
func (r *DelimitedReader) Seek(until byte, backwards bool) (n int, err error) {
	data := r.data
	if len(data) == 0 {
		return 0, ragel.NewReadingError(ragel.ErrNotFound)
	}
	if backwards {
		// Data boundaries
		p := r.p
		i := r.pe - 1

		// Until there are no more bytes or they are different from the the sought one
		for ; i >= p && data[i] != until; i-- {
		}

		// Store the number of sought bytes
		n := r.pe - i

		// Did we find anything?
		if i == p-1 && data[p] != until {
			return r.pe - i, ragel.NewReadingError(ragel.ErrNotFound)
		}

		// Update the right boundary to be the character next to the sought one
		r.pe = i + 1

		return n, nil
	}

	panic("not implemented")
}
