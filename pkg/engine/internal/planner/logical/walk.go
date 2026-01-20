package logical

// walkNode traverses a plan in depth-first order: It starts by calling
// visitor(n). If visitor returns true, walkNode is called recursively for each
// of the non-nil operands of n (if any), followed by a call of visitor(nil).
func walkNode(n Node, visitor func(n Node) bool) {
	if !visitor(n) {
		return
	}

	instr, ok := n.(Instruction)
	if !ok {
		return
	}

	for _, operandPointer := range instr.Operands(nil) {
		operand := *operandPointer
		if operand != nil {
			walkNode(operand, visitor)
		}
	}

	visitor(nil)
}
