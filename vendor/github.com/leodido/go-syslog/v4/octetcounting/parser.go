package octetcounting

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	syslog "github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/auto"
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
	stripTrailingNL  bool // Strip trailing \r\n or \n before passing to inner machine
	// auto-detect fields
	rfc3164Opts []syslog.MachineOption
	rfc5424Opts []syslog.MachineOption
	noFallback  bool
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

	// Octet-counting framing knows the exact message length, so embedded
	// newlines are unambiguous and must be preserved in the MSG field.
	// The trailing \n is stripped before passing to the inner machine since
	// many senders include it as a framing convention, not as message content.
	p.internalOpts = append(p.internalOpts, rfc3164.WithEmbeddedNewlines())
	p.stripTrailingNL = true

	// Create internal parser with machine options
	p.internal = rfc3164.NewMachine(p.internalOpts...)

	return p
}

// NewParserAuto returns a syslog.Parser that auto-detects RFC 3164 vs RFC 5424
// format per-message using octetcounting framing.
//
// Use auto.WithRFC3164MachineOptions and auto.WithRFC5424MachineOptions to
// pass format-specific options. Use auto.WithoutParserFallback to disable
// fallback to the other parser on failure.
func NewParserAuto(opts ...syslog.ParserOption) syslog.Parser {
	p := &parser{
		emit:             func(*syslog.Result) { /* noop */ },
		maxMessageLength: DefaultMaxSize,
	}

	for _, opt := range opts {
		p = opt(p).(*parser)
	}

	// Forward generic machine options (from syslog.WithMachineOptions) to both
	// inner parsers so callers migrating from NewParser get consistent behavior.
	// Options are wrapped with SafeMachineOptions to recover from type-assertion
	// panics when format-specific options are applied to the wrong machine type.
	// Octet-counting framing knows the exact message length, so embedded
	// newlines are unambiguous and must be preserved in the MSG field.
	safeOpts := auto.SafeMachineOptions(p.internalOpts)
	rfc3164Opts := append([]syslog.MachineOption{rfc3164.WithEmbeddedNewlines()}, safeOpts...)
	rfc3164Opts = append(rfc3164Opts, p.rfc3164Opts...)
	rfc5424Opts := append(append([]syslog.MachineOption{}, safeOpts...), p.rfc5424Opts...)

	autoOpts := []auto.Option{
		auto.WithRFC3164Options(rfc3164Opts...),
		auto.WithRFC5424Options(rfc5424Opts...),
	}
	if p.noFallback {
		autoOpts = append(autoOpts, auto.WithoutFallback())
	}

	p.internal = auto.NewMachine(autoOpts...)
	if p.bestEffort {
		p.internal.WithBestEffort()
	}

	// Strip trailing newline (framing convention, not message content).
	p.stripTrailingNL = true

	return p
}

// AutoParserConfigurer implementation for auto-detect parser options.
func (p *parser) SetRFC3164Options(opts []syslog.MachineOption) {
	p.rfc3164Opts = append(p.rfc3164Opts, opts...)
}

func (p *parser) SetRFC5424Options(opts []syslog.MachineOption) {
	p.rfc5424Opts = append(p.rfc5424Opts, opts...)
}

func (p *parser) SetNoFallback() {
	p.noFallback = true
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
	if p.stripTrailingNL && len(input) > 0 {
		// Strip a single trailing \r\n or \n. Many senders include a trailing
		// newline as a framing convention; it is not part of the message content.
		if input[len(input)-1] == '\n' {
			input = input[:len(input)-1]
			if len(input) > 0 && input[len(input)-1] == '\r' {
				input = input[:len(input)-1]
			}
		}
	}
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
