package nontransparent

import (
	syslog "github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/auto"
)

// NewParserAuto returns a syslog.Parser that auto-detects RFC 3164 vs RFC 5424
// format per-message using non-transparent framing (RFC 6587).
//
// Use auto.WithRFC3164MachineOptions and auto.WithRFC5424MachineOptions to
// pass format-specific options. Use auto.WithoutParserFallback to disable
// fallback to the other parser on failure.
func NewParserAuto(options ...syslog.ParserOption) syslog.Parser {
	m := &machine{
		emit: func(*syslog.Result) { /* noop */ },
	}

	for _, opt := range options {
		m = opt(m).(*machine)
	}

	trailer, _ := m.trailertyp.Value()
	m.trailer = byte(trailer)

	// Note: unlike octetcounting, we do NOT inject rfc3164.WithEmbeddedNewlines()
	// here. Non-transparent framing uses LF (or NUL) as the message delimiter,
	// so embedded newlines are inherently ambiguous and terminate the message.
	//
	// Forward generic machine options (from syslog.WithMachineOptions) to both
	// inner parsers so callers migrating from NewParser get consistent behavior.
	// Options are wrapped with SafeMachineOptions to recover from type-assertion
	// panics when format-specific options are applied to the wrong machine type.
	safeOpts := auto.SafeMachineOptions(m.internalOpts)
	rfc3164Opts := append(append([]syslog.MachineOption{}, safeOpts...), m.rfc3164Opts...)
	rfc5424Opts := append(append([]syslog.MachineOption{}, safeOpts...), m.rfc5424Opts...)

	autoOpts := []auto.Option{
		auto.WithRFC3164Options(rfc3164Opts...),
		auto.WithRFC5424Options(rfc5424Opts...),
	}
	if m.noFallback {
		autoOpts = append(autoOpts, auto.WithoutFallback())
	}

	m.internal = auto.NewMachine(autoOpts...)
	if m.bestEffort {
		m.internal.WithBestEffort()
	}

	return m
}

// AutoParserConfigurer implementation for auto-detect parser options.
func (m *machine) SetRFC3164Options(opts []syslog.MachineOption) {
	m.rfc3164Opts = append(m.rfc3164Opts, opts...)
}

func (m *machine) SetRFC5424Options(opts []syslog.MachineOption) {
	m.rfc5424Opts = append(m.rfc5424Opts, opts...)
}

func (m *machine) SetNoFallback() {
	m.noFallback = true
}
