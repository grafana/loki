package logical

import (
	"fmt"
)

// The LOGQL_COMPAT instruction is a marker to indicate v1 engine compatibility.
// LogQLCompat implements [Instruction] and [Value].
type LogQLCompat struct {
	Value Value

	id string
}

// String returns the disassembled SSA form of r.
func (c *LogQLCompat) String() string {
	return fmt.Sprintf("LOGQL_COMPAT %s", c.Value.Name())
}

func (c *LogQLCompat) Name() string {
	if c.id == "" {
		return "LogQL Compatibility"
	}
	return c.id
}

func (c *LogQLCompat) isInstruction() {}
func (c *LogQLCompat) isValue()       {}
