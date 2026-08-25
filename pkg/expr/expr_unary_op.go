package expr

// UnaryOp denotes a unary operation to perform against a single argument.
type UnaryOp int

const (
	// UnaryOpInvalid indicates an invalid unary operation. Evaluating a
	// UnaryOpInvalid will result in an error.
	UnaryOpInvalid UnaryOp = iota

	// UnaryOpNOT represents a logical NOT operation over a boolean value.
	UnaryOpNOT
)

var unaryOpStrings = [...]string{
	UnaryOpInvalid: "INVALID",
	UnaryOpNOT:     "NOT",
}

// String returns the string representation of op. If op is out of bounds, it
// returns "INVALID."
func (op UnaryOp) String() string {
	if op < 0 || int(op) >= len(unaryOpStrings) {
		return "INVALID"
	}
	return unaryOpStrings[op]
}
