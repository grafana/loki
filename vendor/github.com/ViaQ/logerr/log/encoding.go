package log

import (
	"encoding/json"
	"io"
)

// JSONEncoder encodes messages as JSON
type JSONEncoder struct{}

// Encode encodes the message as JSON to w
func (j JSONEncoder) Encode(w io.Writer, entry map[string]interface{}) error {
	return json.NewEncoder(w).Encode(entry)
}

// Encoder encodes messages
type Encoder interface {
	Encode(w io.Writer, entry map[string]interface{}) error
}
