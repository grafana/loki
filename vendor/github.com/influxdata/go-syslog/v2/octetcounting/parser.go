package octetcounting

import (
	"fmt"
	"io"

	syslog "github.com/influxdata/go-syslog/v2"
	"github.com/influxdata/go-syslog/v2/rfc5424"
)

// parser is capable to parse the input stream containing syslog messages with octetcounting framing.
//
// Use NewParser function to instantiate one.
type parser struct {
	msglen     int64
	s          Scanner
	internal   syslog.Machine
	last       Token
	stepback   bool // Wheter to retrieve the last token or not
	bestEffort bool // Best effort mode flag
	emit       syslog.ParserListener
}

// NewParser returns a syslog.Parser suitable to parse syslog messages sent with transparent - ie. octet counting (RFC 5425) - framing.
func NewParser(opts ...syslog.ParserOption) syslog.Parser {
	p := &parser{
		emit: func(*syslog.Result) { /* noop */ },
	}

	for _, opt := range opts {
		p = opt(p).(*parser)
	}

	// Create internal parser depending on options
	if p.bestEffort {
		p.internal = rfc5424.NewMachine(rfc5424.WithBestEffort())
	} else {
		p.internal = rfc5424.NewMachine()
	}

	return p
}

// HasBestEffort tells whether the receiving parser has best effort mode on or off.
func (p *parser) HasBestEffort() bool {
	return p.bestEffort
}

// WithBestEffort implements the syslog.BestEfforter interface.
//
// The generic options uses it.
func (p *parser) WithBestEffort() {
	p.bestEffort = true
}

// WithListener implements the syslog.Parser interface.
//
// The generic options uses it.
func (p *parser) WithListener(f syslog.ParserListener) {
	p.emit = f
}

// Parse parses the io.Reader incoming bytes.
//
// It stops parsing when an error regarding RFC 5425 is found.
func (p *parser) Parse(r io.Reader) {
	p.s = *NewScanner(r)
	p.run()
}

func (p *parser) run() {
	for {
		var tok Token

		// First token MUST be a MSGLEN
		if tok = p.scan(); tok.typ != MSGLEN {
			p.emit(&syslog.Result{
				Error: fmt.Errorf("found %s, expecting a %s", tok, MSGLEN),
			})
			break
		}

		// Next we MUST see a WS
		if tok = p.scan(); tok.typ != WS {
			p.emit(&syslog.Result{
				Error: fmt.Errorf("found %s, expecting a %s", tok, WS),
			})
			break
		}

		// Next we MUST see a SYSLOGMSG with length equal to MSGLEN
		if tok = p.scan(); tok.typ != SYSLOGMSG {
			e := fmt.Errorf(`found %s after "%s", expecting a %s containing %d octets`, tok, tok.lit, SYSLOGMSG, p.s.msglen)
			// Underflow case
			if len(tok.lit) < int(p.s.msglen) && p.bestEffort {
				// Though MSGLEN was not respected, we try to parse the existing SYSLOGMSG as a RFC5424 syslog message
				result := p.parse(tok.lit)
				if result.Error == nil {
					result.Error = e
				}
				p.emit(result)
				break
			}

			p.emit(&syslog.Result{
				Error: e,
			})
			break
		}

		// Parse the SYSLOGMSG literal pretending it is a RFC5424 syslog message
		result := p.parse(tok.lit)
		if p.bestEffort || result.Error == nil {
			p.emit(result)
		}
		if !p.bestEffort && result.Error != nil {
			p.emit(&syslog.Result{Error: result.Error})
			break
		}

		// Next we MUST see an EOF otherwise the parsing we'll start again
		if tok = p.scan(); tok.typ == EOF {
			break
		} else {
			p.unscan()
		}
	}
}

func (p *parser) parse(input []byte) *syslog.Result {
	sys, err := p.internal.Parse(input)

	return &syslog.Result{
		Message: sys,
		Error:   err,
	}
}

// scan returns the next token from the underlying scanner;
// if a token has been unscanned then read that instead.
func (p *parser) scan() Token {
	// If we have a token on the buffer, then return it.
	if p.stepback {
		p.stepback = false
		return p.last
	}

	// Otherwise read the next token from the scanner.
	tok := p.s.Scan()

	// Save it to the buffer in case we unscan later.
	p.last = tok

	return tok
}

// unscan pushes the previously read token back onto the buffer.
func (p *parser) unscan() {
	p.stepback = true
}
