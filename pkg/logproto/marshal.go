package logproto

import (
	"encoding/json"
	fmt "fmt"

	"github.com/prometheus/prometheus/promql"
)

// MarshalJSON converts an Entry object to be prom compatible for http queries
func (e *Entry) MarshalJSON() ([]byte, error) {
	l, err := json.Marshal(e.Line)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("[\"%d\",%s]", e.Timestamp.UnixNano(), l)), nil
}

// MarshalJSON converts a Stream object to be prom compatible for http queries
func (s *Stream) MarshalJSON() ([]byte, error) {
	parsedLabels, err := promql.ParseMetric(s.Labels)
	if err != nil {
		return nil, err
	}
	l, err := json.Marshal(parsedLabels)
	if err != nil {
		return nil, err
	}
	e, err := json.Marshal(s.Entries)
	if err != nil {
		return nil, err
	}

	return []byte(fmt.Sprintf("{\"stream\":%s,\"values\":%s}", l, e)), nil
}
