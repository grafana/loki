package syslogparser

import (
	"bufio"
	"fmt"
	"io"

	"github.com/influxdata/go-syslog/v2"
	"github.com/influxdata/go-syslog/v2/octetcounting"
	"github.com/influxdata/go-syslog/v2/rfc5424"
)

// ParseStream parses a rfc5424 syslog stream from the given Reader, calling
// the callback function with the parsed messages. The parser automatically
// detects octet counting.
// The function returns on EOF or unrecoverable errors.
func ParseStream(r io.Reader, callback func(res *syslog.Result)) error {
	buf := bufio.NewReader(r)

	firstByte, err := buf.Peek(1)
	if err != nil {
		return err
	}

	b := firstByte[0]
	if b == '<' {
		newlineSeparated(buf, callback)
	} else if b >= '0' && b <= '9' {
		octetCounting(buf, callback)
	} else {
		return fmt.Errorf("invalid or unsupported framing. first byte: '%s'", firstByte)
	}

	return nil
}

func octetCounting(r io.Reader, callback func(res *syslog.Result)) {
	octetcounting.NewParser(syslog.WithListener(callback)).Parse(r)
}

func newlineSeparated(r io.Reader, callback func(res *syslog.Result)) {
	s := bufio.NewScanner(r)
	p := rfc5424.NewParser()
	for s.Scan() {
		msg, err := p.Parse(s.Bytes())
		callback(&syslog.Result{Message: msg, Error: err})
	}

	if err := s.Err(); err != nil {
		err = fmt.Errorf("error reading from stream: %w", err)
		callback(&syslog.Result{Error: err})
	}
}
