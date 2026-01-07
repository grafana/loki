package logical

import "fmt"

// baseNode holds common logic for all nodes.
type baseNode struct {
	// referrers of a node. Only populated for nodes that implement [Value].
	referrers []Instruction
	id        string // ID used for instructions
}

// Name returns the id of the node, used when printing instructions.
func (n *baseNode) Name() string {
	if n.id != "" {
		return n.id
	}
	return fmt.Sprintf("%p", n)
}

func (n *baseNode) addReferrer(instr Instruction) {
	n.referrers = append(n.referrers, instr)
}
