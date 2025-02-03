package nontransparent

import (
	"io"

	syslog "github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/rfc3164"
	"github.com/leodido/go-syslog/v4/rfc5424"
	parser "github.com/leodido/ragel-machinery/parser"
)

const nontransparentStart int = 1
const nontransparentError int = 0

const nontransparentEnMain int = 1

type machine struct {
	trailertyp TrailerType // default is 0 thus TrailerType(LF)
	trailer    byte
	candidate  []byte
	bestEffort bool
	internal   syslog.Machine
	emit       syslog.ParserListener
	readError  error
	lastChunk  []byte // store last candidate message also if it does not ends with a trailer
}

// Exec implements the ragel.Parser interface.
func (m *machine) Exec(s *parser.State) (int, int) {
	// Retrieve previously stored parsing variables
	cs, p, pe, eof, data := s.Get()
	{
		var _widec int16
		if p == pe {
			goto _testEof
		}
		switch cs {
		case 1:
			goto stCase1
		case 0:
			goto stCase0
		case 2:
			goto stCase2
		case 3:
			goto stCase3
		}
		goto stOut
	stCase1:
		if data[p] == 60 {
			goto tr0
		}
		goto st0
	stCase0:
	st0:
		cs = 0
		goto _out
	tr0:

		if len(m.candidate) > 0 {
			m.process()
		}
		m.candidate = make([]byte, 0)

		goto st2
	st2:
		if p++; p == pe {
			goto _testEof2
		}
	stCase2:
		_widec = int16(data[p])
		switch {
		case data[p] > 0:
			if 10 <= data[p] && data[p] <= 10 {
				_widec = 256 + (int16(data[p]) - 0)
				if m.trailertyp == LF {
					_widec += 256
				}
			}
		default:
			_widec = 768 + (int16(data[p]) - 0)
			if m.trailertyp == NUL {
				_widec += 256
			}
		}
		switch _widec {
		case 266:
			goto st2
		case 522:
			goto tr3
		case 768:
			goto st2
		case 1024:
			goto tr3
		}
		switch {
		case _widec > 9:
			if 11 <= _widec {
				goto st2
			}
		case _widec >= 1:
			goto st2
		}
		goto st0
	tr3:

		m.candidate = append(m.candidate, data...)

		goto st3
	st3:
		if p++; p == pe {
			goto _testEof3
		}
	stCase3:
		_widec = int16(data[p])
		switch {
		case data[p] > 0:
			if 10 <= data[p] && data[p] <= 10 {
				_widec = 256 + (int16(data[p]) - 0)
				if m.trailertyp == LF {
					_widec += 256
				}
			}
		default:
			_widec = 768 + (int16(data[p]) - 0)
			if m.trailertyp == NUL {
				_widec += 256
			}
		}
		switch _widec {
		case 60:
			goto tr0
		case 266:
			goto st2
		case 522:
			goto tr3
		case 768:
			goto st2
		case 1024:
			goto tr3
		}
		switch {
		case _widec > 9:
			if 11 <= _widec {
				goto st2
			}
		case _widec >= 1:
			goto st2
		}
		goto st0
	stOut:
	_testEof2:
		cs = 2
		goto _testEof
	_testEof3:
		cs = 3
		goto _testEof

	_testEof:
		{
		}
	_out:
		{
		}
	}
	// Update parsing variables
	s.Set(cs, p, pe, eof)
	return p, pe
}

func (m *machine) OnErr(chunk []byte, err error) {
	// Store the last chunk of bytes ending without a trailer - ie., unexpected EOF from the reader
	m.lastChunk = chunk
	m.readError = err
}

func (m *machine) OnEOF(chunk []byte) {
}

func (m *machine) OnCompletion() {
	if len(m.candidate) > 0 {
		m.process()
	}
	// Try to parse last chunk as a candidate
	if m.readError != nil && len(m.lastChunk) > 0 {
		res, err := m.internal.Parse(m.lastChunk)
		if err == nil && !m.bestEffort {
			res = nil
			err = m.readError
		}
		m.emit(&syslog.Result{
			Message: res,
			Error:   err,
		})
	}
}

// NewParser returns a syslog.Parser suitable to parse syslog messages sent with non-transparent framing - ie. RFC 6587.
func NewParser(options ...syslog.ParserOption) syslog.Parser {
	m := &machine{
		emit: func(*syslog.Result) { /* noop */ },
	}

	for _, opt := range options {
		m = opt(m).(*machine)
	}

	// No error can happens since during its setting we check the trailer type passed in
	trailer, _ := m.trailertyp.Value()
	m.trailer = byte(trailer)

	// Create internal parser depending on options
	if m.bestEffort {
		m.internal = rfc5424.NewMachine(rfc5424.WithBestEffort())
	} else {
		m.internal = rfc5424.NewMachine()
	}

	return m
}

func NewParserRFC3164(options ...syslog.ParserOption) syslog.Parser {
	m := &machine{
		emit: func(*syslog.Result) { /* noop */ },
	}

	for _, opt := range options {
		m = opt(m).(*machine)
	}

	// No error can happens since during its setting we check the trailer type passed in
	trailer, _ := m.trailertyp.Value()
	m.trailer = byte(trailer)

	// Create internal parser depending on options
	if m.bestEffort {
		m.internal = rfc3164.NewMachine(rfc3164.WithBestEffort())
	} else {
		m.internal = rfc3164.NewMachine()
	}

	return m
}

// WithMaxMessageLength does nothing for this parser.
func (m *machine) WithMaxMessageLength(length int) {}

// HasBestEffort tells whether the receiving parser has best effort mode on or off.
func (m *machine) HasBestEffort() bool {
	return m.bestEffort
}

// WithTrailer ... todo(leodido)
func WithTrailer(t TrailerType) syslog.ParserOption {
	return func(m syslog.Parser) syslog.Parser {
		if val, err := t.Value(); err == nil {
			m.(*machine).trailer = byte(val)
			m.(*machine).trailertyp = t
		}
		return m
	}
}

// WithBestEffort implements the syslog.BestEfforter interface.
//
// The generic options uses it.
func (m *machine) WithBestEffort() {
	m.bestEffort = true
}

// WithListener implements the syslog.Parser interface.
//
// The generic options uses it.
func (m *machine) WithListener(f syslog.ParserListener) {
	m.emit = f
}

// Parse parses the io.Reader incoming bytes.
//
// It stops parsing when an error regarding RFC 6587 is found.
func (m *machine) Parse(reader io.Reader) {
	r := parser.ArbitraryReader(reader, m.trailer)
	parser.New(r, m, parser.WithStart(1)).Parse()
}

func (m *machine) process() {
	lastByte := len(m.candidate) - 1
	if m.candidate[lastByte] == m.trailer {
		m.candidate = m.candidate[:lastByte]
	}
	res, err := m.internal.Parse(m.candidate)
	m.emit(&syslog.Result{
		Message: res,
		Error:   err,
	})
}
