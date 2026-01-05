package logical

// baseNode holds common logic for all nodes.
type baseNode struct {
	// referrers of a node. Only populated for nodes that implement [Value].
	referrers []Instruction
}

func (n *baseNode) addReferrer(instr Instruction) {
	n.referrers = append(n.referrers, instr)
}
