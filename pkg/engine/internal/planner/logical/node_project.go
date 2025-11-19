package logical

import (
	"fmt"
	"strings"
)

// The Projection instruction projects (keeps/drops) columns from a relation.
// Projection implements both [Instruction] and [Value].
type Projection struct {
	id string

	Relation    Value   // The input relation.
	Expressions []Value // The expressions to apply for projecting columns.

	All    bool // Marker for projecting all columns of input relation (similar to SQL `SELECT *`)
	Expand bool // Indicates that projected columns should be added to input relation
	Drop   bool // Indicates that projected columns should be dropped from input Relation
}

var (
	_ Value       = (*Projection)(nil)
	_ Instruction = (*Projection)(nil)
)

// Name returns an identifier for the Projection operation.
func (p *Projection) Name() string {
	if p.id != "" {
		return p.id
	}
	return fmt.Sprintf("%p", p)
}

// String returns the disassembled SSA form of the Projection instruction.
func (p *Projection) String() string {
	params := make([]string, 0, len(p.Expressions)+1)
	params = append(params, fmt.Sprintf("mode=%s", p.mode()))
	for _, expr := range p.Expressions {
		params = append(params, fmt.Sprintf("expr=%s", expr.String()))
	}
	return fmt.Sprintf("PROJECT %s [%s]", p.Relation.Name(), strings.Join(params, ", "))
}

func (p *Projection) mode() string {
	var mode string
	if p.All {
		mode += "*"
	}
	if p.Expand {
		mode += "E"
	}
	if p.Drop {
		mode += "D"
	}
	return mode
}

func (p *Projection) isInstruction() {}
func (p *Projection) isValue()       {}
