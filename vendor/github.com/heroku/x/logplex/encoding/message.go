package encoding

import (
	"io"
	"time"
)

// Message is a syslog message
type Message struct {
	Timestamp    time.Time
	Hostname     string
	Application  string
	Process      string
	ID           string
	Message      string
	Version      uint16
	Priority     uint8
	RFCCompliant bool
}

// Size returns the message size in bytes, including the octet framing header
func (m Message) Size() (int, error) {
	b, err := Encode(m)
	if err != nil {
		return 0, err
	}

	return len(b), nil
}

// WriteTo writes the message to a stream
func (m Message) WriteTo(w io.Writer) (int64, error) {
	b, err := Encode(m)
	if err != nil {
		return 0, err
	}

	i, err := w.Write(b)
	return int64(i), err
}
