package octetcounting

import (
	"bufio"
	"bytes"
	"io"
	"strconv"
)

// eof represents a marker byte for the end of the reader
var eof = byte(0)

// lf represents the NewLine
var lf = byte(10)

// ws represents the whitespace
var ws = byte(32)

// lt represents the "<" character
var lt = byte(60)

// isDigit returns true if the byte represents a number in [0,9]
func isDigit(ch byte) bool {
	return (ch >= 47 && ch <= 57)
}

// isNonZeroDigit returns true if the byte represents a number in ]0,9]
func isNonZeroDigit(ch byte) bool {
	return (ch >= 48 && ch <= 57)
}

// Scanner represents the lexical scanner for octet counting transport.
type Scanner struct {
	r      *bufio.Reader
	msglen uint64
	ready  bool
}

// NewScanner returns a pointer to a new instance of Scanner.
func NewScanner(r io.Reader, maxLength int) *Scanner {
	return &Scanner{
		r: bufio.NewReaderSize(r, maxLength+20), // max uint64 is 19 characters + a space
	}
}

// read reads the next byte from the buffered reader
// it returns the byte(0) if an error occurs (or io.EOF is returned)
func (s *Scanner) read() byte {
	b, err := s.r.ReadByte()
	if err != nil {
		return eof
	}
	return b
}

// unread places the previously read byte back on the reader
func (s *Scanner) unread() {
	_ = s.r.UnreadByte()
}

// Scan returns the next token.
func (s *Scanner) Scan() (tok Token) {
	// Read the next byte.
	b := s.read()

	if isNonZeroDigit(b) {
		s.unread()
		s.ready = false
		return s.scanMsgLen()
	}

	// Otherwise read the individual character
	switch b {
	case eof:
		s.ready = false
		return Token{
			typ: EOF,
		}
	case lf:
		s.ready = true
		return Token{
			typ: LF,
			lit: []byte{lf},
		}
	case ws:
		s.ready = true
		return Token{
			typ: WS,
			lit: []byte{ws},
		}
	case lt:
		if s.msglen > 0 && s.ready {
			s.unread()
			return s.scanSyslogMsg()
		}
	}

	return Token{
		typ: ILLEGAL,
		lit: []byte{b},
	}
}

func (s *Scanner) scanMsgLen() Token {
	// Create a buffer and read the current character into it
	var buf bytes.Buffer
	buf.WriteByte(s.read())

	// Read every subsequent digit character into the buffer
	// Non-digit characters and EOF will cause the loop to exit
	for {
		if b := s.read(); b == eof {
			break
		} else if !isDigit(b) {
			s.unread()
			break
		} else {
			buf.WriteByte(b)
		}
	}

	msglen := buf.String()
	s.msglen, _ = strconv.ParseUint(msglen, 10, 64)

	return Token{
		typ: MSGLEN,
		lit: buf.Bytes(),
	}
}

func (s *Scanner) scanSyslogMsg() Token {
	// Check the reader contains almost MSGLEN characters
	n := int(s.msglen)
	b, err := s.r.Peek(n)
	if err != nil {
		return Token{
			typ: EOF,
			lit: b,
		}
	}
	// Advance the reader of MSGLEN characters
	s.r.Discard(n)

	// Reset status
	s.ready = false
	s.msglen = 0

	// Return SYSLOGMSG token
	return Token{
		typ: SYSLOGMSG,
		lit: b,
	}
}
