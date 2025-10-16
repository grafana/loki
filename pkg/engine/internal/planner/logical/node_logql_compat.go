package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/internal/planner/schema"
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

func (c *LogQLCompat) Schema() *schema.Schema {
	return c.Value.Schema()
}

func (c *LogQLCompat) isInstruction() {}
func (c *LogQLCompat) isValue()       {}
