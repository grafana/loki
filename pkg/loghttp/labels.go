package loghttp

import (
	"bytes"
	"net/http"
	"sort"
	"strconv"

	"github.com/gorilla/mux"

	"github.com/grafana/loki/pkg/logproto"
)

// LabelResponse represents the http json response to a label query
type LabelResponse struct {
	Status string   `json:"status"`
	Data   []string `json:"data,omitempty"`
}

// LabelSet is a key/value pair mapping of labels
type LabelSet map[string]string

// Map coerces LabelSet into a map[string]string. This is useful for working with adapter types.
func (l LabelSet) Map() map[string]string {
	return l
}

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

// ParseLabelQuery parses a LabelRequest request from an http request.
func ParseLabelQuery(r *http.Request) (*logproto.LabelRequest, error) {
	name, ok := mux.Vars(r)["name"]
	req := &logproto.LabelRequest{
		Values: ok,
		Name:   name,
	}

	start, end, err := bounds(r)
	if err != nil {
		return nil, err
	}
	req.Start = &start
	req.End = &end
	return req, nil
}
