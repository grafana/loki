package pattern

import "fmt"

type node interface {
	fmt.Stringer
}

type expr []node

func (e expr) hasCapture() bool {
	for _, n := range e {
		if c, ok := n.(capture); ok && !c.isUnamed() {
			return true
		}
	}
	return false
}

type capture string

func (c capture) String() string {
	return string(c)
}

func (c capture) isUnamed() bool {
	return string(c) == tokens[UNDERSCORE]
}

type literal rune

func (l literal) String() string {
	return string(l)
}

func literals(in string) []node {
	res := make([]node, len(in))
	for i, r := range in {
		res[i] = literal(r)
	}
	return res
}
