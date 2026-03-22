package nontransparent

import (
	"fmt"
	"strings"
)

// TrailerType is the king of supported trailers for non-transparent frames.
type TrailerType int

const (
	// LF is the line feed - ie., byte 10. Also the default one.
	LF TrailerType = iota
	// NUL is the nul byte - ie., byte 0.
	NUL
)

var names = [...]string{"LF", "NUL"}
var bytes = []int{10, 0}

func (t TrailerType) String() string {
	if t < LF || t > NUL {
		return ""
	}

	return names[t]
}

// Value returns the byte corresponding to the receiving TrailerType.
func (t TrailerType) Value() (int, error) {
	if t < LF || t > NUL {
		return -1, fmt.Errorf("unknown TrailerType")
	}

	return bytes[t], nil
}

// TrailerTypeFromString returns a TrailerType given a string.
func TrailerTypeFromString(s string) (TrailerType, error) {
	switch strings.ToUpper(s) {
	case `"LF"`:
		fallthrough
	case `'LF'`:
		fallthrough
	case `LF`:
		return LF, nil

	case `"NUL"`:
		fallthrough
	case `'NUL'`:
		fallthrough
	case `NUL`:
		return NUL, nil
	}
	return -1, fmt.Errorf("unknown TrailerType")
}

// UnmarshalTOML decodes trailer type from TOML data.
func (t *TrailerType) UnmarshalTOML(data []byte) (err error) {
	return t.UnmarshalText(data)
}

// UnmarshalText implements encoding.TextUnmarshaler
func (t *TrailerType) UnmarshalText(data []byte) (err error) {
	*t, err = TrailerTypeFromString(string(data))
	return err
}

// MarshalText implements encoding.TextMarshaler
func (t TrailerType) MarshalText() ([]byte, error) {
	s := t.String()
	if s != "" {
		return []byte(s), nil
	}
	return nil, fmt.Errorf("unknown TrailerType")
}
