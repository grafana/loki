package columnar

import "fmt"

type errorSliceBounds struct {
	i, j   int
	maxLen int
}

func (e errorSliceBounds) Error() string {
	return fmt.Sprintf("slice [%d:%d] out of range of max length %d", e.i, e.j, e.maxLen)
}
