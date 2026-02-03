package expr

// BinaryOp denotes a binary operation to perform against two arguments.
type BinaryOp int

const (
	// BinaryOpInvalid indicates an invalid binary operation. Evaluating a
	// BinaryOpInvalid will result in an error.
	BinaryOpInvalid BinaryOp = iota

	// BinaryOpEQ performs an equality (==) check of the left and right
	// expressions. The expressions must be of the same type.
	//
	// The result is a bool datum, which is either a bool scalar if both
	// arguments are scalars, otherwise the result is a bool array.
	BinaryOpEQ

	// BinaryOpNEQ performs an inequality (!=) check of the left and right
	// expressions. The expressions must be of the same type.
	//
	// The result is a bool datum, which is either a bool scalar if both
	// arguments are scalars, otherwise the result is a bool array.
	BinaryOpNEQ

	// BinaryOpGT performs a greater than (>) check of the left and right
	// expressions. The expressions must be of the same type, and must be
	// ordered (numeric or UTF8).
	//
	// The result is a bool datum, which is either a bool scalar if both
	// arguments are scalars, otherwise the result is a bool array.
	BinaryOpGT

	// BinaryOpGTE performs a greater than or equal (>=) check of the left and
	// right expressions. The expressions must be of the same type, and must be
	// ordered (numeric or UTF8).
	//
	// The result is a bool datum, which is either a bool scalar if both
	// arguments are scalars, otherwise the result is a bool array.
	BinaryOpGTE

	// BinaryOpLT performs a less than (<) check of the left and right
	// expressions. The expressions must be of the same type, and must be
	// ordered (numeric or UTF8).
	//
	// The result is a bool datum, which is either a bool scalar if both
	// arguments are scalars, otherwise the result is a bool array.
	BinaryOpLT

	// BinaryOpLTE performs a less than or equal (<=) check of the left and
	// right expressions. The expressions must be of the same type, and must be
	// ordered (numeric or UTF8).
	//
	// The result is a bool datum, which is either a bool scalar if both
	// arguments are scalars, otherwise the result is a bool array.
	BinaryOpLTE

	// BinaryOpAND performs a logical AND (&&) operation on the left and right
	// expressions. The expressions must be of bool type.
	//
	// The result is a bool datum, which is either a bool scalar if both
	// arguments are scalars, otherwise the result is a bool array.
	BinaryOpAND

	// BinaryOpOR performs a logical OR (||) operation on the left and right
	// expressions. The expressions must be of bool type.
	//
	// The result is a bool datum, which is either a bool scalar if both
	// arguments are scalars, otherwise the result is a bool array.
	BinaryOpOR

	// BinaryOpHasSubstrIgnoreCase performs a case-insensitive substring check
	// of the left and right expressions.
	//
	// The left expression denotes the "haystack" to search, and must be a UTF8
	// scalar or array. The right expression denotes the "needle" to search
	// with, and must be a UTF8 scalar. If the needle is found in the haystack
	// (ignoring case), the result is true.
	//
	// The result is a bool datum, which is either a bool scalar if both
	// arguments are scalars, otherwise the result is a bool array.
	BinaryOpHasSubstrIgnoreCase
)

var binaryOpStrings = [...]string{
	BinaryOpInvalid: "INVALID",

	BinaryOpEQ:  "EQ",
	BinaryOpNEQ: "NEQ",
	BinaryOpGT:  "GT",
	BinaryOpGTE: "GTE",
	BinaryOpLT:  "LT",
	BinaryOpLTE: "LTE",

	BinaryOpAND: "AND",
	BinaryOpOR:  "OR",

	BinaryOpHasSubstrIgnoreCase: "HAS_SUBSTR_IGNORECASE",
}

// String returns the string representation of op. If op is out of bounds, it
// returns "INVALID."
func (op BinaryOp) String() string {
	if op < 0 || int(op) >= len(binaryOpStrings) {
		return "INVALID"
	}
	return binaryOpStrings[op]
}
