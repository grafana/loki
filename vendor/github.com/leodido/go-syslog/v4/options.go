package syslog

// WithListener returns a generic option that sets the emit function for syslog parsers.
func WithListener(f ParserListener) ParserOption {
	return func(p Parser) Parser {
		p.WithListener(f)
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

// WithMachineOptions returns a generic option that sets the machine options for syslog parsers.
func WithMachineOptions(opts ...MachineOption) ParserOption {
	return func(p Parser) Parser {
		p.WithMachineOptions(opts...)
		return p
	}
}
