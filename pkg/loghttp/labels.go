package loghttp

import (
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gorilla/mux"
	"github.com/grafana/jsonparser"

	"github.com/grafana/loki/v3/pkg/logproto"
)

// LabelResponse represents the http json response to a label query
type LabelResponse struct {
	Status string   `json:"status"`
	Data   []string `json:"data,omitempty"`
}

// LabelSet is a key/value pair mapping of labels
type LabelSet map[string]string

func (l *LabelSet) UnmarshalJSON(data []byte) error {
	if *l == nil {
		*l = make(LabelSet)
	}
	return jsonparser.ObjectEach(data, func(key, val []byte, _ jsonparser.ValueType, _ int) error {
		v, err := jsonparser.ParseString(val)
		if err != nil {
			return err
		}
		k, err := jsonparser.ParseString(key)
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
