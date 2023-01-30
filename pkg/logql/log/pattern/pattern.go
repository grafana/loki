package pattern

import (
	"bytes"
	"errors"
)

var (
	ErrNoCapture   = errors.New("at least one capture is required")
	ErrInvalidExpr = errors.New("invalid expression")
)

type Matcher interface {
	Matches(in []byte) [][]byte
	Names() []string
}

type matcher struct {
	e expr

	captures [][]byte
	names    []string
}

func New(in string) (Matcher, error) {
	e, err := parseExpr(in)
	if err != nil {
		return nil, err
	}
	if err := e.validate(); err != nil {
		return nil, err
	}
	return &matcher{
		e:        e,
		captures: make([][]byte, 0, e.captureCount()),
		names:    e.captures(),
	}, nil
}

// Matches matches the given line with the provided pattern.
// Matches invalidates the previous returned captures array.
func (m *matcher) Matches(in []byte) [][]byte {
	if len(in) == 0 {
		return nil
	}
	if len(m.e) == 0 {
		return nil
	}
	captures := m.captures[:0]
	expr := m.e
	if ls, ok := expr[0].(literals); ok {
		i := bytes.Index(in, ls)
		if i != 0 {
			return nil
		}
		in = in[len(ls):]
		expr = expr[1:]
	}
	if len(expr) == 0 {
		return nil
	}
	// from now we have capture - literals - capture ... (literals)?
	for len(expr) != 0 {
		if len(expr) == 1 { // we're ending on a capture.
			if !(expr[0].(capture)).isUnamed() {
				captures = append(captures, in)
			}
			return captures
		}
		cap := expr[0].(capture)
		ls := expr[1].(literals)
		expr = expr[2:]
		i := bytes.Index(in, ls)
		if i == -1 {
			// if a capture is missed we return up to the end as the capture.
			if !cap.isUnamed() {
				captures = append(captures, in)
			}
			return captures
		}

		if cap.isUnamed() {
			in = in[len(ls)+i:]
			continue
		}
		captures = append(captures, in[:i])
		in = in[len(ls)+i:]
	}

	return captures
}

func (m *matcher) Names() []string {
	return m.names
}
