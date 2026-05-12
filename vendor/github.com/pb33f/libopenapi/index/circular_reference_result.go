package index

import (
	"strings"

	"go.yaml.in/yaml/v4"
)

// CircularReferenceResult contains a circular reference found when traversing the graph.
type CircularReferenceResult struct {
	Journey             []*Reference
	ParentNode          *yaml.Node
	Start               *Reference
	LoopIndex           int
	LoopPoint           *Reference
	IsArrayResult       bool   // if this result comes from an array loop.
	PolymorphicType     string // which type of polymorphic loop is this? (oneOf, anyOf, allOf)
	IsPolymorphicResult bool   // if this result comes from a polymorphic loop.
	IsInfiniteLoop      bool   // if all the definitions in the reference loop are marked as required, this is an infinite circular reference, thus is not allowed.
}

// GenerateJourneyPath generates a string representation of the journey taken to find the circular reference.
func (c *CircularReferenceResult) GenerateJourneyPath() string {
	buf := strings.Builder{}
	for i, ref := range c.Journey {
		if i > 0 {
			buf.WriteString(" -> ")
		}

		buf.WriteString(ref.Name)
	}

	return buf.String()
}
