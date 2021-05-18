package pattern

import (
	"bytes"
	"errors"
)

var ErrNoCapture = errors.New("at least one capture is required")

type Matcher interface {
	Matches(in []byte) [][]byte
}

type matcher struct {
	e expr
}

func New(in string) (Matcher, error) {
	e, err := parseExpr(in)
	if err != nil {
		return nil, err
	}
	if !e.hasCapture() {
		return nil, ErrNoCapture
	}
	return &matcher{e: e}, nil
}

func (m *matcher) Matches(in []byte) [][]byte {
	if len(in) == 0 {
		return nil
	}
	if len(m.e) == 0 {
		return nil
	}
	captures := make([][]byte, 0, m.e.captureCount())
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
	for len(expr) != 0 || len(in) != 0 {
		if len(expr) == 1 { // we're ending on a capture.
			captures = append(captures, in)
			return captures
		}
		cap := expr[0].(capture)
		ls := expr[1].(literals)
		expr = expr[2:]
		i := bytes.Index(in, ls)
		if i == -1 {
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
