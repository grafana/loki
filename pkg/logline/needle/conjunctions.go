package needle

import (
	"strconv"
	"strings"
)

// Conjunctions is an OR of literal sets: a line must contain every literal of
// at least one set. An empty outer slice means TRUE, or no lookup constraint.
// Compile returns sorted, normalized, caller-owned slices with no empty sets.
// Callers sharing a result between goroutines must treat it as read-only.
type Conjunctions [][]string

// String formats c as {"a", "b"} or {"c"}, or as TRUE when it is empty.
func (c Conjunctions) String() string {
	if len(c) == 0 {
		return "TRUE"
	}
	var out strings.Builder
	for i, conjunction := range c {
		if i > 0 {
			out.WriteString(" or ")
		}
		out.WriteByte('{')
		for j, literal := range conjunction {
			if j > 0 {
				out.WriteString(", ")
			}
			out.WriteString(strconv.Quote(literal))
		}
		out.WriteByte('}')
	}
	return out.String()
}
