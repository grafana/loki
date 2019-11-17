package syslogparser

import (
	"bufio"
	"fmt"
	"io"

	"github.com/influxdata/go-syslog"
	"github.com/influxdata/go-syslog/octetcounting"
	"github.com/influxdata/go-syslog/rfc5424"
)

// ParseStream parses a rfc5424 syslog stream from the given Reader, returning a channel
// emitting the parsed messages. The parser automatically detects octet counting.
// The channel is closed on EOF or unrecoverable errors.
func ParseStream(r io.Reader) <-chan *syslog.Result {
	results := make(chan *syslog.Result)
	var err error
	defer func() {
		if err == nil {
			return
		}
		go func(err error) {
			results <- &syslog.Result{Error: err}
			close(results)
		}(err)
	}()

	buf := bufio.NewReader(r)
	firstByte, err := buf.Peek(1)
	if err != nil {
		return results
	}

	b := firstByte[0]
	if b == '<' {
		go newlineSeparated(buf, results)
	} else if b >= '0' && b <= '9' {
		go octetCounting(buf, results)
	} else {
		err = fmt.Errorf("invalid or unsupported framing. first byte: '%s'", firstByte)
	}

	return results
}

func octetCounting(r *bufio.Reader, results chan<- *syslog.Result) {
	defer close(results)

	callback := func(res *syslog.Result) {
		results <- res
	}

	octetcounting.NewParser(syslog.WithListener(callback)).Parse(r)
}

func newlineSeparated(r *bufio.Reader, results chan<- *syslog.Result) {
	defer close(results)

	s := bufio.NewScanner(r)
	p := rfc5424.NewParser()
	for s.Scan() {
		msg, err := p.Parse(s.Bytes())
		results <- &syslog.Result{Message: msg, Error: err}
	}

	if err := s.Err(); err != nil {
		err = fmt.Errorf("error reading from stream: %w", err)
		results <- &syslog.Result{Error: err}
	}
}
