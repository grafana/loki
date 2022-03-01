package deletion

import (
	"errors"
)

type Mode int16

var (
	errUnknownMode = errors.New("unknown deletion mode")
)

const (
	Disabled Mode = iota
	V1            // The existing experimental log deletion that removes whole streams.
	FilterOnly
	FilterAndDelete
)

func (m Mode) String() string {
	switch m {
	case Disabled:
		return "disabled"
	case V1:
		return "v1"
	case FilterOnly:
		return "filter-only"
	case FilterAndDelete:
		return "filter-and-delete"
	}
	return "unknown"
}

func AllModes() []string {
	return []string{Disabled.String(), V1.String(), FilterOnly.String(), FilterAndDelete.String()}
}

func ParseMode(in string) (Mode, error) {
	switch in {
	case "disabled":
		return Disabled, nil
	case "v1":
		return V1, nil
	case "filter-only":
		return FilterOnly, nil
	case "filter-and-delete":
		return FilterAndDelete, nil
	}
	return 0, errUnknownMode
}
