package types

import "fmt"

// GroupingMode represents the grouping by/without label(s) for vector aggregators and range vector aggregators.
type GroupingMode int

const (
	GroupingModeInvalid         GroupingMode = iota
	GroupingModeByEmptySet                   // Grouping by empty label set: <operation> by () (<expr>)
	GroupingModeByLabelSet                   // Grouping by label set: <operation> by (<labels...>) (<expr>)
	GroupingModeWithoutEmptySet              // Grouping without empty label set: <operation> without () (<expr>)
	GroupingModeWithoutLabelSet              // Grouping without label set: <operation> without (<labels...>) (<expr>)
)

// String returns the string representation of the GroupingMode.
func (t GroupingMode) String() string {
	switch t {
	case GroupingModeByEmptySet:
		return "GROUPING_BY_EMPTY_SET"
	case GroupingModeByLabelSet:
		return "GROUPING_BY_LABEL_SET"
	case GroupingModeWithoutEmptySet:
		return "GROUPING_WITHOUT_EMPTY_SET"
	case GroupingModeWithoutLabelSet:
		return "GROUPING_WITHOUT_LABEL_SET"
	default:
		panic(fmt.Sprintf("unknown grouping mode %d", t))
	}
}
