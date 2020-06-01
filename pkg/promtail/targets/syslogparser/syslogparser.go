package syslogparser

import (
	"bufio"
	"fmt"
	"io"

	"github.com/influxdata/go-syslog/v3"
	"github.com/influxdata/go-syslog/v3/nontransparent"
	"github.com/influxdata/go-syslog/v3/octetcounting"
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
		nontransparent.NewParser(syslog.WithListener(callback)).Parse(buf)
	} else if b >= '0' && b <= '9' {
		octetcounting.NewParser(syslog.WithListener(callback)).Parse(buf)
	} else {
		return fmt.Errorf("invalid or unsupported framing. first byte: '%s'", firstByte)
	}

	return nil
}
