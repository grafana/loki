package logical

import (
	"fmt"
)

// convertToPlan converts a [Value] into a [Plan]. The value becomes the last
// instruction returned by [Return].
func convertToPlan(value Value) (*Plan, error) {
	var builder ssaBuilder
	builder.process(value)

	// Add the final Return instruction based on the last value.
	builder.instructions = append(builder.instructions, &Return{Value: value})

	return &Plan{Instructions: builder.instructions}, nil
}

// ssaBuilder is a helper type for building SSA forms
type ssaBuilder struct {
	instructions []Instruction
	nextID       int
	seen         map[Node]struct{}
}

func (b *ssaBuilder) getID() int {
	b.nextID++
	return b.nextID
}

// processPlan processes a logical plan and returns the resulting Value.
func (b *ssaBuilder) process(value Node) {
	if b.seen == nil {
		b.seen = make(map[Node]struct{})
	}

	instr, isInstruction := value.(Instruction)
	if !isInstruction {
		// Nothing to do.
		return
	}

	// Process children first.
	for _, operandPointer := range instr.Operands(nil) {
		operand := *operandPointer
		if operand != nil {
			b.process(operand)
		}
	}

	// If this is the first time we've seen this node, give it a new ID.
	//
	// We only do this for values which are also instructions, since they appear
	// as parameters in other values/instructions.
	if _, ok := b.seen[value]; !ok {
		value.base().id = fmt.Sprintf("%%%d", b.getID())

		b.instructions = append(b.instructions, instr)
		b.seen[value] = struct{}{}
	}
}
