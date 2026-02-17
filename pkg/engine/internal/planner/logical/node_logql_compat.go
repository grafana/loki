package logical

import (
	"fmt"
)

// The LOGQL_COMPAT instruction is a marker to indicate v1 engine compatibility.
// LogQLCompat implements [Instruction] and [Value].
type LogQLCompat struct {
	b baseNode

	Value Value
}

// String returns the disassembled SSA form of r.
func (c *LogQLCompat) String() string {
	return fmt.Sprintf("LOGQL_COMPAT %s", c.Value.Name())
}

func (c *LogQLCompat) Name() string { return c.b.Name() }

// Operands appends the operands of c to the provided slice. The pointers may
// be modified to change operands of c.
func (c *LogQLCompat) Operands(buf []*Value) []*Value {
	return append(buf, &c.Value)
}

// Referrers returns a list of instructions that reference the LogQLCompat.
//
// The list of instructions can be modified to update the reference list, such
// as when modifying the plan.
func (c *LogQLCompat) Referrers() *[]Instruction { return &c.b.referrers }

func (c *LogQLCompat) base() *baseNode { return &c.b }
func (c *LogQLCompat) isInstruction()  {}
func (c *LogQLCompat) isValue()        {}
