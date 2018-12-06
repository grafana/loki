package flagext

import (
	"fmt"
)

// Strings is a list of strings.
type Strings []string

// String implements flag.Value
func (ss Strings) String() string {
	return fmt.Sprintf("%s", []string(ss))
}

// Set implements flag.Value
func (ss *Strings) Set(s string) error {
	*ss = append(*ss, s)
	return nil
}
