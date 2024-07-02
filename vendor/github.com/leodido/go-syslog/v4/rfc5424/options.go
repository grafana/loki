package rfc5424

import (
	syslog "github.com/leodido/go-syslog/v4"
)

// WithBestEffort enables the best effort mode.
func WithBestEffort() syslog.MachineOption {
	return func(m syslog.Machine) syslog.Machine {
		m.WithBestEffort()
		return m
	}
}

// WithCompliantMsg enables the parsing of the MSG part of the Syslog as per RFC5424.
//
// When this is on, the MSG can either be:
// - an UTF-8 string which starts with a BOM marker
// or
// - a free-form message (0-255) not starting with a BOM marker.
//
// Ref.: https://tools.ietf.org/html/rfc5424#section-6.4
// Ref.: https://tools.ietf.org/html/rfc5424#section-6
func WithCompliantMsg() syslog.MachineOption {
	return func(m syslog.Machine) syslog.Machine {
		m.(*machine).compliantMsg = true
		return m
	}
}
