package logproto

import (
	"encoding/json"
	fmt "fmt"
)

// MarshalJSON converts an Entry object to be prom compatible for http queries
func (e *Entry) MarshalJSON() ([]byte, error) {
	t, err := json.Marshal(float64(e.Timestamp.UnixNano()) / 1e+9)
	if err != nil {
		return nil, err
	}
	l, err := json.Marshal(e.Line)
	if err != nil {
		return nil, err
	}
	return []byte(fmt.Sprintf("[%s,%s]", t, l)), nil
}

// MarshalJSON converts a Stream object to be prom compatible for http queries
func (s *Stream) MarshalJSON() ([]byte, error) {
	l, err := json.Marshal(s.Labels)
	if err != nil {
		return nil, err
	}
	e, err := json.Marshal(s.Entries)
	if err != nil {
		return nil, err
	}

	return []byte(fmt.Sprintf("{\"labels\":%s,\"values\":%s}", l, e)), nil
}
