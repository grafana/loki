package rfc3164

import (
	"sync"

	syslog "github.com/leodido/go-syslog/v4"
)

// parser represent a RFC3164 parser with mutex capabilities.
type parser struct {
	sync.Mutex
	*machine
}

// NewParser creates a syslog.Machine that parses RFC3164 syslog messages.
func NewParser(options ...syslog.MachineOption) syslog.Machine {
	p := &parser{
		machine: NewMachine(options...).(*machine),
	}

	return p
}

// HasBestEffort tells whether the receiving parser has best effort mode on or off.
func (p *parser) HasBestEffort() bool {
	return p.bestEffort
}

// Parse parses the input RFC3164 syslog message using its FSM.
//
// Best effort mode enables the partial parsing.
func (p *parser) Parse(input []byte) (syslog.Message, error) {
	p.Lock()
	defer p.Unlock()

	msg, err := p.machine.Parse(input)
	if err != nil {
		if p.bestEffort {
			return msg, err
		}
		return nil, err
	}

	return msg, nil
}
