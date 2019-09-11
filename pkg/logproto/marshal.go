package logproto

import (
	"encoding/json"
	fmt "fmt"
)

// MasrshalJSON converts an Entry object to be prom compatible for http queries
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
