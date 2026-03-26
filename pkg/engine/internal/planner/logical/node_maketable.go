package logical

import (
	"fmt"
	"strings"
)

// The MakeTable instruction yields a table relation from an identifier.
// MakeTable implements both [Instruction] and [Value].
type MakeTable struct {
	b baseNode

	// Selector is used to generate a table relation. All streams for which the
	// selector passes are included in the resulting table.
	//
	// It is invalid for Selector to include a [ColumnRef] that is not
	// [ColumnTypeBuiltin] or [ColumnTypeLabel].
	Selector Value

	// Predicates are used to further filter the Selector table relation by utilising additional indexes in the catalogue.
	// Unlike the Selector, there are no restrictions on the type of [ColumnRef] in Predicates.
	Predicates []Value

	// Shard is used to indicate that the table relation does not contain all data
	// of the relation but only a subset of it.
	// The Shard value must be of type [ShardRef].
	Shard Value
}

var (
	_ Value       = (*MakeTable)(nil)
	_ Instruction = (*MakeTable)(nil)
)

// Name returns an identifier for the MakeTable operation.
func (t *MakeTable) Name() string { return t.b.Name() }

// String returns the disassembled SSA form of the MakeTable instruction.
func (t *MakeTable) String() string {
	predicateNames := make([]string, len(t.Predicates))
	for i, predicate := range t.Predicates {
		predicateNames[i] = predicate.Name()
	}
	return fmt.Sprintf("MAKETABLE [selector=%s, predicates=[%s], shard=%s]", t.Selector.Name(), strings.Join(predicateNames, ", "), t.Shard.Name())
}

// Operands appends the operands of t to the provided slice. The pointers may
// be modified to change operands of t.
func (t *MakeTable) Operands(buf []*Value) []*Value {
	buf = append(buf, &t.Selector)
	for i := range t.Predicates {
		buf = append(buf, &t.Predicates[i])
	}
	buf = append(buf, &t.Shard)
	return buf
}

// Referrers returns a list of instructions that reference the MakeTable.
//
// The list of instructions can be modified to update the reference list, such
// as when modifying the plan.
func (t *MakeTable) Referrers() *[]Instruction { return &t.b.referrers }

func (t *MakeTable) base() *baseNode { return &t.b }
func (t *MakeTable) isInstruction()  {}
func (t *MakeTable) isValue()        {}
