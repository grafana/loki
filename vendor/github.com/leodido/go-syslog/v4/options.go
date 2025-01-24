package syslog

// WithListener returns a generic option that sets the emit function for syslog parsers.
func WithListener(f ParserListener) ParserOption {
	return func(p Parser) Parser {
		p.WithListener(f)
		return p
	}
}

// WithBestEffort returns a generic options that enables best effort mode for syslog parsers.
//
// When passed to a parser it tries to recover as much of the syslog messages as possible.
func WithBestEffort() ParserOption {
	return func(p Parser) Parser {
		p.WithBestEffort()
		return p
	}
}

// WithMaxMessageLength sets the length of the buffer for octect parsing.
func WithMaxMessageLength(length int) ParserOption {
	return func(p Parser) Parser {
		p.WithMaxMessageLength(length)
		return p
	}
}
