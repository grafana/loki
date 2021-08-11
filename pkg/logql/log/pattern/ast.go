package pattern

import (
	"fmt"
	"unicode/utf8"
)

type node interface {
	fmt.Stringer
}

type expr []node

func (e expr) hasCapture() bool {
	return e.captureCount() != 0
}

func (e expr) validate() error {
	if !e.hasCapture() {
		return ErrNoCapture
	}
	// if there is at least 2 node, verify that none are consecutive.
	if len(e) >= 2 {
		for i := 0; i < len(e); i = i + 2 {
			if i+1 >= len(e) {
				break
			}
			if _, ok := e[i].(capture); ok {
				if _, ok := e[i+1].(capture); ok {
					return fmt.Errorf("found consecutive capture: %w", ErrInvalidExpr)
				}
			}
		}
	}
	caps := e.captures()
	uniq := map[string]struct{}{}
	for _, c := range caps {
		if _, ok := uniq[c]; ok {
			return fmt.Errorf("duplicate capture name (%s): %w", c, ErrInvalidExpr)
		}
		uniq[c] = struct{}{}
	}
	return nil
}

func (e expr) captures() (captures []string) {
	for _, n := range e {
		if c, ok := n.(capture); ok && !c.isUnamed() {
			captures = append(captures, c.String())
		}
	}
	return
}

func (e expr) captureCount() (count int) {
	return len(e.captures())
}

type capture string

func (c capture) String() string {
	return string(c)
}

func (c capture) isUnamed() bool {
	return string(c) == underscore
}

type literals []byte

func (l literals) String() string {
	return string(l)
}

func runesToLiterals(rs []rune) literals {
	res := make([]byte, len(rs)*utf8.UTFMax)
	count := 0
	for _, r := range rs {
		count += utf8.EncodeRune(res[count:], r)
	}
	res = res[:count]
	return res
}
