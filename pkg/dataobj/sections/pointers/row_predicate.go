package pointers

type (
	// RowPredicate is an expression used to filter rows in a data object.
	RowPredicate interface{ isRowPredicate() }
)

// Supported predicates.
type (
	// A BloomExistencePredicate is a RowPredicate which requires a bloom filter column named
	// Name to exist, and for the Value to pass the bloom filter.
	BloomExistencePredicate struct{ Name, Value string }
)

func (BloomExistencePredicate) isRowPredicate() {}
