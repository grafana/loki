package computetest

import (
	"io"

	"github.com/grafana/loki/v3/pkg/columnar"
	"github.com/grafana/loki/v3/pkg/memory"
)

// Case is a parsed test case.
type Case struct {
	Line int // Source line of the test case.

	Function  string
	Arguments []columnar.Datum
	Selection memory.Bitmap
	Expect    columnar.Datum
}

// ParseCases parses all test cases from the given reader.
func ParseCases(r io.Reader) ([]Case, error) {
	p := &parser{
		scanner: newScanner(r),
		alloc:   memory.NewAllocator(nil),
	}

	return p.Parse()
}
