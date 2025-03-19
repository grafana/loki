package logical

import (
	"fmt"

	"github.com/grafana/loki/v3/pkg/engine/planner/schema"
)

// The Select instruction filters rows from a table relation. Select implements
// both [Instruction] and [Value].
type Select struct {
	id string

	Input TreeNode // Child plan node providing data to filter.
	Expr  Expr     // Boolean expression used to filter nodes.
}

var (
	_ Value       = (*Select)(nil)
	_ Instruction = (*Select)(nil)
)

// newSelect creates a new Select plan node.
// It takes an input plan and a boolean expression that determines which rows
// should be selected (included) in its output. This is represented by the WHERE
// clause in SQL. A simple example would be SELECT * FROM foo WHERE a > 5.
// The filter expression needs to evaluate to a Boolean result.
func newSelect(input TreeNode, expr Expr) *Select {
	return &Select{
		Input: input,
		Expr:  expr,
	}
}

// Name returns an identifier for the Select operation.
func (s *Select) Name() string {
	if s.id != "" {
		return s.id
	}
	return fmt.Sprintf("<%p>", s)
}

// String returns the disassembled SSA form of the Select instruction.
func (s *Select) String() string {
	// TODO(rfratto): change the type of s.Expr to [Value] so we can call
	// s.Expr.Name here.
	return fmt.Sprintf("select %v", s.Expr)
}

// Schema returns the schema of the Select plan.
// The schema of a Select is the same as the schema of its input,
// as selection only removes rows and doesn't modify the structure.
func (s *Select) Schema() *schema.Schema {
	res := s.Input.Schema()
	return &res
}

func (s *Select) isInstruction() {}
func (s *Select) isValue()       {}
