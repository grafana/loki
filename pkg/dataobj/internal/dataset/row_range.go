package dataset

import (
	"fmt"
)

// rowRange denotes an inclusive range of rows [Start, End].
type rowRange struct {
	Start, End uint64
}

// String prints out the row range in a human-readable format
// "[rr.Start,rr.End]".
func (rr rowRange) String() string {
	return fmt.Sprintf("[%d,%d]", rr.Start, rr.End)
}

// GoString prints out the row range in a human-readable format, useful for
// debugging and in test results.
func (rr rowRange) GoString() string {
	return rr.String()
}

// Contains returns true if a row is inside a range.
func (rr rowRange) Contains(row uint64) bool {
	return row >= rr.Start && row <= rr.End
}

// ContainsRange returns true if all rows in other are inside rr.
func (rr rowRange) ContainsRange(other rowRange) bool {
	return rr.Start <= other.Start && rr.End >= other.End
}

// Overlaps returns true if any row in other is inside rr.
func (rr rowRange) Overlaps(other rowRange) bool {
	// Two ranges overlap if the maximum start is less than or equal to the
	// minimum end:
	//
	// See these cases where S is max(a.Start, b.Start) and E is min(a.End,
	// b.End):
	//
	// a: [-----]
	// b:   [-----]
	//      ^   ^
	//      S   E         <- S < E, so a and b overlap.
	//
	// a: [-----]
	// b:       [------]
	//          ^
	//         SE         <- S == E, so a and b overlap.
	//
	// a:         [-----]
	// b: [-----]
	//          ^ ^
	//          E S       <- S > E, so a and b don't overlap.
	return max(rr.Start, other.Start) <= min(rr.End, other.End)
}
