package deletionmode

import (
	"errors"
	"fmt"
	"strings"
)

type Mode int16

var (
	ErrUnknownMode = errors.New("unknown deletion mode")
)

const (
	disabled        = "disabled"
	filterOnly      = "filter-only"
	filterAndDelete = "filter-and-delete"
	unknown         = "unknown"

	Disabled Mode = iota
	FilterOnly
	FilterAndDelete
)

func (m Mode) String() string {
	switch m {
	case Disabled:
		return disabled
	case FilterOnly:
		return filterOnly
	case FilterAndDelete:
		return filterAndDelete
	}
	return unknown
}

func (m Mode) DeleteEnabled() bool {
	return m == FilterOnly || m == FilterAndDelete
}

func AllModes() []string {
	return []string{Disabled.String(), FilterOnly.String(), FilterAndDelete.String()}
}

func ParseMode(in string) (Mode, error) {
	switch in {
	case disabled:
		return Disabled, nil
	case filterOnly:
		return FilterOnly, nil
	case filterAndDelete:
		return FilterAndDelete, nil
	}
	return 0, fmt.Errorf("%w: must be one of %s", ErrUnknownMode, strings.Join(AllModes(), "|"))
}

func Enabled(in string) (bool, error) {
	deleteMode, err := ParseMode(in)
	if err != nil {
		return false, err
	}

	return deleteMode.DeleteEnabled(), nil
}
