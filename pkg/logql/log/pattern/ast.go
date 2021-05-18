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

func (e expr) captureCount() (count int) {
	for _, n := range e {
		if c, ok := n.(capture); ok && !c.isUnamed() {
			count++
		}
	}
	return
}

type capture string

func (c capture) String() string {
	return string(c)
}

func (c capture) isUnamed() bool {
	return string(c) == tokens[UNDERSCORE]
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
