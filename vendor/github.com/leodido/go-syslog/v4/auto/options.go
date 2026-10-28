package auto

import syslog "github.com/leodido/go-syslog/v4"

// Option configures the auto-detect machine.
type Option func(*machine)

// WithRFC3164Options sets MachineOptions for the inner RFC 3164 machine.
func WithRFC3164Options(opts ...syslog.MachineOption) Option {
	return func(m *machine) {
		m.rfc3164Opts = append(m.rfc3164Opts, opts...)
	}
}

// WithRFC5424Options sets MachineOptions for the inner RFC 5424 machine.
func WithRFC5424Options(opts ...syslog.MachineOption) Option {
	return func(m *machine) {
		m.rfc5424Opts = append(m.rfc5424Opts, opts...)
	}
}

// WithoutFallback disables fallback to the other parser on complete failure.
// By default, if the peek-chosen parser fails, the other parser is tried.
func WithoutFallback() Option {
	return func(m *machine) {
		m.noFallback = true
	}
}

// WithRFC3164MachineOptions returns a syslog.ParserOption that sets
// MachineOptions for the inner RFC 3164 machine in transport auto-detect parsers.
func WithRFC3164MachineOptions(opts ...syslog.MachineOption) syslog.ParserOption {
	return func(p syslog.Parser) syslog.Parser {
		if ap, ok := p.(AutoParserConfigurer); ok {
			ap.SetRFC3164Options(opts)
		}
		return p
	}
}

// WithRFC5424MachineOptions returns a syslog.ParserOption that sets
// MachineOptions for the inner RFC 5424 machine in transport auto-detect parsers.
func WithRFC5424MachineOptions(opts ...syslog.MachineOption) syslog.ParserOption {
	return func(p syslog.Parser) syslog.Parser {
		if ap, ok := p.(AutoParserConfigurer); ok {
			ap.SetRFC5424Options(opts)
		}
		return p
	}
}

// WithoutParserFallback returns a syslog.ParserOption that disables fallback
// in transport auto-detect parsers.
func WithoutParserFallback() syslog.ParserOption {
	return func(p syslog.Parser) syslog.Parser {
		if ap, ok := p.(AutoParserConfigurer); ok {
			ap.SetNoFallback()
		}
		return p
	}
}

// AutoParserConfigurer is implemented by transport auto-detect parsers
// to accept format-specific configuration.
type AutoParserConfigurer interface {
	SetRFC3164Options(opts []syslog.MachineOption)
	SetRFC5424Options(opts []syslog.MachineOption)
	SetNoFallback()
}
