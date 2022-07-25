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
	FilterOnly
	FilterAndDelete
)

func (m Mode) String() string {
	switch m {
	case Disabled:
		return "disabled"
	case FilterOnly:
		return "filter-only"
	case FilterAndDelete:
		return "filter-and-delete"
	}
	return "unknown"
}

func (m Mode) DeleteEnabled() bool {
	return m == FilterOnly || m == FilterAndDelete
}

func AllModes() []string {
	return []string{Disabled.String(), FilterOnly.String(), FilterAndDelete.String()}
}

func ParseMode(in string) (Mode, error) {
	switch in {
	case "disabled":
		return Disabled, nil
	case "filter-only":
		return FilterOnly, nil
	case "filter-and-delete":
		return FilterAndDelete, nil
	}
	return 0, errUnknownMode
}

func DeleteEnabled(in string) (bool, error) {
	deleteMode, err := ParseMode(in)
	if err != nil {
		return false, err
	}

	return deleteMode.DeleteEnabled(), nil
}
