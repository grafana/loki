package nontransparent

import (
    "io"

    parser "github.com/leodido/ragel-machinery/parser"
    syslog "github.com/leodido/go-syslog/v4"
    "github.com/leodido/go-syslog/v4/rfc5424"
    "github.com/leodido/go-syslog/v4/rfc3164"
)

%%{
machine nontransparent;

# unsigned alphabet
alphtype uint8;

action on_trailer {
    m.candidate = append(m.candidate, data...)
}

action on_init {
    if len(m.candidate) > 0 {
        m.process()
    }
    m.candidate = make([]byte, 0)
}

t = 10 when { m.trailertyp == LF } |
    00 when { m.trailertyp == NUL };

main :=
    start: (
        '<' >on_init (any)* -> trailer
    ),
    trailer: (
        t >on_trailer -> final |
        t >on_trailer -> start
    );

}%%

%% write data nofinal;

type machine struct{
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
    %% write exec;
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
            Error: err,
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
    parser.New(r, m, parser.WithStart(%%{ write start; }%%)).Parse()
}

func (m *machine) process() {
    lastByte := len(m.candidate) - 1
    if m.candidate[lastByte] == m.trailer {
        m.candidate = m.candidate[:lastByte]
    }
    res, err := m.internal.Parse(m.candidate)
    m.emit(&syslog.Result{
        Message: res,
        Error: err,
    })
}
