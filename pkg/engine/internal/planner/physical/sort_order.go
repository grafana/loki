package physical

type SortOrder uint8

const (
	UNSORTED SortOrder = iota
	ASC
	DESC
)

// String returns the string representation of the [SortOrder].
func (o SortOrder) String() string {
	switch o {
	case UNSORTED:
		return "UNSORTED"
	case ASC:
		return "ASC"
	case DESC:
		return "DESC"
	default:
		return "UNDEFINED"
	}
}
