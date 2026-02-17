package octetcounting

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	syslog "github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/rfc3164"
	"github.com/leodido/go-syslog/v4/rfc5424"
)

// DefaultMaxSize as per RFC5425#section-4.3.1
const DefaultMaxSize = 8192

// parser is capable to parse the input stream containing syslog messages with octetcounting framing.
//
// Use NewParser function to instantiate one.
type parser struct {
	maxMessageLength int
	s                Scanner
	bestEffort       bool
	internal         syslog.Machine
	internalOpts     []syslog.MachineOption
	last             Token
	stepback         bool // Wheter to retrieve the last token or not
	emit             syslog.ParserListener
}

// NewParser returns a syslog.Parser suitable to parse syslog messages sent with transparent - ie. octet counting (RFC 5425) - framing.
func NewParser(opts ...syslog.ParserOption) syslog.Parser {
	p := &parser{
		emit:             func(*syslog.Result) { /* noop */ },
		maxMessageLength: DefaultMaxSize,
	}

	for _, opt := range opts {
		p = opt(p).(*parser)
	}

	// If bestEffort flag was set, add it to options
	if p.bestEffort {
		p.internalOpts = append(p.internalOpts, rfc5424.WithBestEffort())
	}

	// Create internal parser with options
	p.internal = rfc5424.NewMachine(p.internalOpts...)

	return p
}

func NewParserRFC3164(opts ...syslog.ParserOption) syslog.Parser {
	p := &parser{
		emit:             func(*syslog.Result) { /* noop */ },
		maxMessageLength: DefaultMaxSize,
	}

	for _, opt := range opts {
		p = opt(p).(*parser)
	}

	// If bestEffort flag was set, add it to options
	if p.bestEffort {
		p.internalOpts = append(p.internalOpts, rfc3164.WithBestEffort())
	}

	// Create internal parser with machine options
	p.internal = rfc3164.NewMachine(p.internalOpts...)

	return p
}

// WithBestEffort implements the syslog.BestEfforter interface.
func (p *parser) WithBestEffort() {
	p.bestEffort = true
}

// HasBestEffort tells whether the receiving parser has best effort mode on or off.
func (p *parser) HasBestEffort() bool {
	return p.internal.HasBestEffort()
}

// WithMachineOptions configures options for the underlying parsing machine.
func (p *parser) WithMachineOptions(opts ...syslog.MachineOption) {
	p.internalOpts = append(p.internalOpts, opts...)
}

func (p *parser) WithMaxMessageLength(length int) {
	p.maxMessageLength = length
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
	p.s = *NewScanner(r, p.maxMessageLength)
	p.run()
}

func (p *parser) run() {
	defer p.s.Release()
	for {
		var tok Token

		// First token MUST be a MSGLEN
		if tok = p.scan(); tok.typ != MSGLEN {
			if tok.typ == ILLEGAL {
				if bytes.Equal(tok.lit, ErrMsgInvalidLength) {
					p.emit(&syslog.Result{
						Error: errors.New(string(ErrMsgInvalidLength)),
					})
					break
				} else if bytes.Equal(tok.lit, ErrMsgTooLarge) {
					p.emit(&syslog.Result{
						Error: fmt.Errorf(string(ErrMsgTooLarge), p.s.msglen, p.maxMessageLength),
					})
					break
				} else if bytes.Equal(tok.lit, ErrMsgExceedsIntLimit) {
					p.emit(&syslog.Result{
						Error: errors.New(string(ErrMsgExceedsIntLimit)),
					})
					break
				}
			}

			// Default error case
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
			if len(tok.lit) < int(p.s.msglen) && p.internal.HasBestEffort() {
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
		if p.internal.HasBestEffort() || result.Error == nil {
			p.emit(result)
		}
		if !p.internal.HasBestEffort() && result.Error != nil {
			p.emit(&syslog.Result{Error: result.Error})
			break
		}

		// Next we MUST see an EOF otherwise the parsing we'll start again
		if tok = p.scan(); tok.typ == EOF {
			break
		} else if tok.typ != LF {
			// but some syslog may separate lines with octet by \n, ignore it
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
