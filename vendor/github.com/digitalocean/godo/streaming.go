package godo

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
)

// SSEEvent is a dispatched Server-Sent Events event. Data is the joined
// value of all "data:" fields, separated by '\n', and is only valid until
// the next call to SSEReader.Next.
type SSEEvent struct {
	Event string
	Data  []byte
	ID    string
	Retry int
}

// SSEReader parses a "text/event-stream" byte stream into SSEEvents.
// Bare-CR line terminators are not supported. Not safe for concurrent use.
type SSEReader struct {
	r       *bufio.Reader
	lastID  string
	scratch []byte
	err     error
}

// NewSSEReader returns an SSEReader that reads from r.
func NewSSEReader(r io.Reader) *SSEReader {
	br, ok := r.(*bufio.Reader)
	if !ok {
		br = bufio.NewReaderSize(r, 16*1024)
	}
	return &SSEReader{r: br}
}

// Next returns the next dispatched event, or io.EOF when the stream
// ends. A final event without a trailing blank line is dispatched on
// the call that hits EOF; the subsequent call returns io.EOF.
func (s *SSEReader) Next() (*SSEEvent, error) {
	if s.err != nil {
		return nil, s.err
	}

	s.scratch = s.scratch[:0]
	var (
		eventTyp string
		retry    int
		haveData bool
		haveAny  bool
	)

	for {
		line, err := s.readLine()
		eof := err == io.EOF

		if len(line) == 0 {
			if haveAny {
				ev := s.makeEvent(eventTyp, retry)
				if eof {
					s.err = io.EOF
				}
				return ev, nil
			}
			if eof {
				s.err = io.EOF
				return nil, io.EOF
			}
			if err != nil {
				s.err = err
				return nil, err
			}
			continue
		}

		if line[0] == ':' {
			if eof {
				s.err = io.EOF
				return nil, io.EOF
			}
			continue
		}

		field, value := splitField(line)
		haveAny = true
		switch field {
		case "data":
			if haveData {
				s.scratch = append(s.scratch, '\n')
			}
			s.scratch = append(s.scratch, value...)
			haveData = true
		case "event":
			eventTyp = string(value)
		case "id":
			if bytes.IndexByte(value, 0) < 0 {
				s.lastID = string(value)
			}
		case "retry":
			if n, perr := strconv.Atoi(string(value)); perr == nil && n >= 0 {
				retry = n
			}
		}

		if eof {
			ev := s.makeEvent(eventTyp, retry)
			s.err = io.EOF
			return ev, nil
		}
		if err != nil {
			s.err = err
			return nil, err
		}
	}
}

func (s *SSEReader) makeEvent(eventTyp string, retry int) *SSEEvent {
	if eventTyp == "" {
		eventTyp = "message"
	}
	return &SSEEvent{
		Event: eventTyp,
		Data:  s.scratch,
		ID:    s.lastID,
		Retry: retry,
	}
}

func (s *SSEReader) readLine() ([]byte, error) {
	line, err := s.r.ReadBytes('\n')
	if len(line) > 0 && line[len(line)-1] == '\n' {
		line = line[:len(line)-1]
	}
	if len(line) > 0 && line[len(line)-1] == '\r' {
		line = line[:len(line)-1]
	}
	return line, err
}

func splitField(line []byte) (string, []byte) {
	i := bytes.IndexByte(line, ':')
	if i < 0 {
		return string(line), nil
	}
	value := line[i+1:]
	if len(value) > 0 && value[0] == ' ' {
		value = value[1:]
	}
	return string(line[:i]), value
}
