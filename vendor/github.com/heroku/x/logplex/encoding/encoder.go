package encoding

import (
	"fmt"
	"io"
	"strconv"

	"github.com/pkg/errors"
)

// SyslogTimeFormat defines the exact time format used in our logs.
const SyslogTimeFormat = "2006-01-02T15:04:05.999999-07:00"

// FlexibleSyslogTimeFormat accepts both 'Z' and TZ notation for event time.
const FlexibleSyslogTimeFormat = "2006-01-02T15:04:05.999999Z07:00"

// HumanTimeFormat defines the human friendly time format used in CLI/UI.
const HumanTimeFormat = "2006-01-02T15:04:05.000000-07:00"

// L15Error is the message returned with an L15 error
const L15Error = "L15: Error displaying log lines. Please try again."

// ErrInvalidMessage returned when trying to encode an invalid syslog message
var ErrInvalidMessage = errors.New("invalid message")

// Encoder abstracts away how messages are written out
type Encoder interface {
	Encode(msg Message) error
	KeepAlive() error
}

type plainEncoder struct {
	w io.Writer
}

// NewPlain creates a plain encoder. It dumps the log message directly
// without massaging it
func NewPlain(w io.Writer) Encoder {
	return &plainEncoder{w}
}

// Encode writes the message as-is
func (p *plainEncoder) Encode(msg Message) error {
	_, err := p.w.Write([]byte(messageToString(msg)))
	return err
}

// KeepAlive sends a null byte.
func (p *plainEncoder) KeepAlive() error {
	_, err := p.w.Write([]byte{0})
	return err
}

// sseEncoder wraps an io.Writer and provides convenience methods for SSE
type sseEncoder struct {
	w io.Writer
}

// NewSSE instantiates a new SSE encoder
func NewSSE(w io.Writer) Encoder {
	return &sseEncoder{w}
}

// KeepAlive sends a blank comment.
func (s *sseEncoder) KeepAlive() error {
	_, err := fmt.Fprintf(s.w, ": \n")
	return err
}

// Encode assembles the message according to the SSE spec and writes it out
func (s *sseEncoder) Encode(msg Message) error {
	// Use time as the base for creating an ID, since we need monotonic numbers that we can potentially do offsets from
	s.id(msg.Timestamp.Unix())
	s.data(msg)
	s.separator()
	return nil
}

func (s *sseEncoder) id(id int64) {
	fmt.Fprintf(s.w, "id: %v\n", id)
}

func (s *sseEncoder) data(msg Message) {
	fmt.Fprint(s.w, "data: ")
	fmt.Fprint(s.w, messageToString(msg))
	fmt.Fprint(s.w, "\n")
}

func (s *sseEncoder) separator() {
	fmt.Fprint(s.w, "\n\n")
}

func messageToString(msg Message) string {
	return msg.Timestamp.Format(HumanTimeFormat) + " " + msg.Application + "[" + msg.Process + "]: " + msg.Message
}

// Encode serializes a syslog message into their wire format ( octet-framed syslog )
// Disabling RFC 5424 compliance is the default and needed due to https://github.com/heroku/logplex/issues/204
func Encode(msg Message) ([]byte, error) {
	sd := ""
	if msg.RFCCompliant {
		sd = "- "
	}

	if msg.Version == 0 {
		return nil, errors.Wrap(ErrInvalidMessage, "version")
	}

	line := "<" + strconv.Itoa(int(msg.Priority)) + ">" + strconv.Itoa(int(msg.Version)) + " " +
		msg.Timestamp.Format(SyslogTimeFormat) + " " +
		stringOrNil(msg.Hostname) + " " +
		stringOrNil(msg.Application) + " " +
		stringOrNil(msg.Process) + " " +
		stringOrNil(msg.ID) + " " +
		sd +
		msg.Message

	return []byte(strconv.Itoa(len(line)) + " " + line), nil
}

func stringOrNil(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
