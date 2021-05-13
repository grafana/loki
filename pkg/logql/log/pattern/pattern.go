package pattern

import "errors"

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
	return nil
}
