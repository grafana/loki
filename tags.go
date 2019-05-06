package chunk

import (
	"fmt"
	"strings"
)

// Tags is a string-string map that implements flag.Value.
type Tags map[string]string

// String implements flag.Value
func (ts Tags) String() string {
	if ts == nil {
		return ""
	}

	return fmt.Sprintf("%v", map[string]string(ts))
}

// Set implements flag.Value
func (ts *Tags) Set(s string) error {
	if *ts == nil {
		*ts = map[string]string{}
	}

	parts := strings.SplitN(s, "=", 2)
	if len(parts) != 2 {
		return fmt.Errorf("tag must of the format key=value")
	}
	(*ts)[parts[0]] = parts[1]
	return nil
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (ts *Tags) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var m map[string]string
	if err := unmarshal(&m); err != nil {
		return err
	}
	*ts = Tags(m)
	return nil
}

// Equals returns true is other matches ts.
func (ts Tags) Equals(other Tags) bool {
	if len(ts) != len(other) {
		return false
	}

	for k, v1 := range ts {
		v2, ok := other[k]
		if !ok || v1 != v2 {
			return false
		}
	}

	return true
}
