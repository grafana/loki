package loghttp

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gorilla/mux"

	"github.com/grafana/loki/v3/pkg/logproto"
	"github.com/grafana/loki/v3/pkg/util/json"
)

// LabelResponse represents the http json response to a label query
type LabelResponse struct {
	Status string   `json:"status"`
	Data   []string `json:"data,omitempty"`
}

// LabelSet is a key/value pair mapping of labels
type LabelSet map[string]string

func (l *LabelSet) UnmarshalJSON(data []byte) error {
	d := json.GetDecoder(data)
	defer d.Release()
	return l.decode(d)
}

// decode reads an object from d. Non-string values keep their raw JSON text.
func (l *LabelSet) decode(d *json.Decoder) error {
	if *l == nil {
		*l = make(LabelSet)
	}
	return d.EachField(func(key []byte) error {
		k := string(key) // copy: reading the value reuses the buffer
		v, err := d.ReadScalar()
		if err != nil {
			return err
		}
		(*l)[k] = v
		return nil
	})
}

// Map coerces LabelSet into a map[string]string. This is useful for working with adapter types.
func (l LabelSet) Map() map[string]string {
	return l
}

// String implements the Stringer interface.  It returns a formatted/sorted set of label key/value pairs.
func (l LabelSet) String() string {
	var b strings.Builder

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

	req.Query = query(r)
	return req, nil
}

func ParseDetectedLabelsQuery(r *http.Request) (*logproto.DetectedLabelsRequest, error) {
	var err error

	start, end, err := bounds(r)
	if err != nil {
		return nil, err
	}

	if end.Before(start) {
		return nil, errors.New("end timestamp must not be before or equal to start time")
	}

	return &logproto.DetectedLabelsRequest{
		Start: start,
		End:   end,
		Query: query(r),
	}, nil
}
