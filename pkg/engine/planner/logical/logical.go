package logical

import "github.com/grafana/loki/v3/pkg/engine/planner/schema"

// An Instruction is an SSA instruction that computes a new [Value] or has some
// effect.
//
// Instructions that define a value (e.g., BinOp) also implement the Value
// interface; an Instruction that only has an effect (e.g., Return) does not.
type Instruction interface {
	// String returns the disassembled SSA form of the Instruction. This does not
	// include the name of the Value if the Instruction also implements [Value].
	String() string

	// isInstruction is a marker method to prevent external implementations.
	isInstruction()
}

// A Value is an SSA value that can be referenced by an [Instruction].
type Value interface {
	// Name returns an identifier for this Value (such as "%1"), which is used
	// when this Value appears as an operand of an Instruction.
	//
	// If the Value was not created by the logical planner, Name instead returns
	// the pointer address of the Value.
	Name() string

	// String returns human-readable information about the Value. If Value also
	// implements [Instruction], String returns the disassembled form of the
	// Instruction as documented by [Instruction.String].
	String() string

	// Schema returns the type of this Value.
	Schema() *schema.Schema

	// isValue is a marker method to prevent external implementations.
	isValue()
}

// A Plan represents a sequence of [Instruction]s that ultimately produce a
// [Value].
//
// The first [Return] instruction in the plan denotes the final output.
type Plan struct {
	Instructions []Instruction // Instructions of the plan in order.
}
