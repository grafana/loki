package parser

import (
	"github.com/leodido/ragel-machinery"
	"io"
)

// Parser creates ragel parsers for stream inputs.
//
// Its scope is to let the user concentrate on the definition of the ragel machines.
// To do so it allows the user to specify (and delegates to)
// how to read the input chunks and how to parse them.
type Parser parser

type parser struct {
	reader  Reader   // Reader to which to delegate the reading logic
	machine Machiner // FSM to which to delegate the parsing logic

	*parsingState
}

// Option represents an option for parser brokers.
type Option func(*parser)

// WithStart serves to specify the ragel start state.
func WithStart(cs int) Option {
	return func(p *parser) {
		p.cs = cs
	}
}

// WithError serves to specify the ragel error state.
func WithError(err int) Option {
	return func(p *parser) {
		p.errorState = err
	}
}

// WithFirstFinal serves to specify the ragel first final state.
func WithFirstFinal(f int) Option {
	return func(p *parser) {
		p.finalState = f
	}
}

// New cretes a new Parser.
func New(r Reader, m Machiner, opts ...Option) *Parser {
	p := &parser{
		reader:  r,
		machine: m,
	}

	// Tell the broker to use the reader status for parsing
	p.parsingState = (*parsingState)(r.State())

	// Apply options
	for _, opt := range opts {
		opt(p)
	}

	return (*Parser)(p)
}

// Parse is the standard parsing method for stream inputs.
//
// It calls the Read method of the Reader, which defines how and what to read
// and then it calls on such data window the finite-state machine to parse its content.
// It stops whenever and EOF or an error happens.
func (p *Parser) Parse() {
	for {
		res, err := p.reader.Read()
		if err != nil {
			if err == io.EOF {
				p.machine.OnEOF(res)
			} else {
				p.machine.OnErr(res, ragel.NewReadingError(err.Error()))
			}
			break
		}
		// Execute the FSM
		p.machine.Exec((*State)(p.parsingState))
	}
	p.machine.OnCompletion()
}
