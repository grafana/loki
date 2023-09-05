package loghttp

import (
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/buger/jsonparser"
	"github.com/gorilla/mux"
	"github.com/prometheus/prometheus/model/labels"

	"github.com/grafana/loki/pkg/logproto"
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

func (l LabelSet) ToProto() []logproto.LabelAdapter {
	if len(l) == 0 {
		return nil
	}
	return logproto.FromLabelsToLabelAdapters(labels.FromMap(l.Map()))
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

type CategorizedLabelSet struct {
	Stream             LabelSet `json:"stream,omitempty"`
	StructuredMetadata LabelSet `json:"structuredMetadata,omitempty"`
	Parsed             LabelSet `json:"parsed,omitempty"`
}

func (c *CategorizedLabelSet) Empty() bool {
	return len(c.Stream) == 0 && len(c.StructuredMetadata) == 0 && len(c.Parsed) == 0
}

func (c *CategorizedLabelSet) ToProto() logproto.CategorizedLabels {
	return logproto.CategorizedLabels{
		Stream:             c.Stream.ToProto(),
		StructuredMetadata: c.StructuredMetadata.ToProto(),
		Parsed:             c.Parsed.ToProto(),
	}
}

func (c *CategorizedLabelSet) ToLabelSet() LabelSet {
	ret := make(LabelSet, len(c.Stream)+len(c.StructuredMetadata)+len(c.Parsed))
	for k, v := range c.Stream {
		ret[k] = v
	}
	for k, v := range c.StructuredMetadata {
		ret[k] = v
	}
	for k, v := range c.Parsed {
		ret[k] = v
	}
	return ret
}

func (c *CategorizedLabelSet) UnmarshalJSON(data []byte) error {
	return jsonparser.ObjectEach(data, func(key, val []byte, _ jsonparser.ValueType, _ int) error {
		switch string(key) {
		case "stream":
			if err := c.Stream.UnmarshalJSON(val); err != nil {
				return err
			}
		case "structuredMetadata":
			if err := c.StructuredMetadata.UnmarshalJSON(val); err != nil {
				return err
			}
		case "parsed":
			if err := c.Parsed.UnmarshalJSON(val); err != nil {
				return err
			}
		}
		return nil
	})
}
