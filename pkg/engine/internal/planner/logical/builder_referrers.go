package logical

// buildReferrers traverses instrs and stores referrers to values, such that
// [Value.Referrers] produces the correct result.
func buildReferrers(instrs ...Instruction) {
	seen := make(map[Instruction]struct{})
	var operandsBuf []*Value

	for _, check := range instrs {
		walkNode(check, func(n Node) bool {
			instr, ok := n.(Instruction)
			if n == nil || !ok {
				return true
			}

			// Don't process the same instruction more than once.
			if _, ok := seen[instr]; ok {
				return true
			}
			seen[instr] = struct{}{}

			operandsBuf = instr.Operands(operandsBuf[:0])

			for _, operandPointer := range operandsBuf {
				operand := *operandPointer
				if operand == nil {
					continue
				}
				operand.base().addReferrer(instr)
			}

			return true
		})
	}
}
