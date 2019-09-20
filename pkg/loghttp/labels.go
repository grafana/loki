package loghttp

import (
	"bytes"
	"sort"
	"strconv"
)

// LabelResponse represents the http json response to a label query
type LabelResponse struct {
	Values []string `json:"values,omitempty"`
}

// LabelSet is a key/value pair mapping of labels
type LabelSet map[string]string

// String implements the Stringer interface.  It returns a formatted/sorted set of label key/value pairs.
func (l LabelSet) String() string {
	var b bytes.Buffer

	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
			b.WriteByte(' ')
		}
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(strconv.Quote(l[k]))
	}
	b.WriteByte('}')
	return b.String()
}
