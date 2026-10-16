package dataset

// FoldAndPredicate returns left AND right, folding away a constant operand.
func FoldAndPredicate(left, right Predicate) Predicate {
	if keep, ok := IsConstPredicate(left); ok {
		if !keep {
			return FalsePredicate{}
		}
		return right
	}
	if keep, ok := IsConstPredicate(right); ok {
		if !keep {
			return FalsePredicate{}
		}
		return left
	}
	return AndPredicate{Left: left, Right: right}
}

// FoldOrPredicate returns left OR right, folding away a constant operand.
func FoldOrPredicate(left, right Predicate) Predicate {
	if keep, ok := IsConstPredicate(left); ok {
		if keep {
			return TruePredicate{}
		}
		return right
	}
	if keep, ok := IsConstPredicate(right); ok {
		if keep {
			return TruePredicate{}
		}
		return left
	}
	return OrPredicate{Left: left, Right: right}
}

// FoldNotPredicate returns NOT inner, folding a constant inner into the opposite constant.
func FoldNotPredicate(inner Predicate) Predicate {
	if keep, ok := IsConstPredicate(inner); ok {
		return NewConstPredicate(!keep)
	}
	return NotPredicate{Inner: inner}
}
