package variant

// Loc describes where the value of a variant path lives for one row
// (or one list element) of a columnar reader window.
type Loc uint8

const (
	// LocMissing means the path has no value: an enclosing level is null,
	// the field is absent from its parent object, or the whole variant
	// column is null for the row.
	LocMissing Loc = iota

	// LocNull means the path holds the variant null value.
	LocNull

	// LocTyped means the value is a scalar stored in a shredded typed_value
	// column; it appears in the cursor's typed vector.
	LocTyped

	// LocTypedObject means the value is an object stored (at least
	// partially) in shredded form. Leftover fields of a partially shredded
	// object are available through the residual.
	LocTypedObject

	// LocTypedList means the value is a list stored in shredded form.
	LocTypedList

	// LocResidual means the value exists only as variant binary at this
	// position.
	LocResidual
)

func (l Loc) String() string {
	switch l {
	case LocMissing:
		return "missing"
	case LocNull:
		return "null"
	case LocTyped:
		return "typed"
	case LocTypedObject:
		return "typed-object"
	case LocTypedList:
		return "typed-list"
	case LocResidual:
		return "residual"
	default:
		return "unknown"
	}
}
