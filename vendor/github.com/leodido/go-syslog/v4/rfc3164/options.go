package rfc3164

import (
	"time"

	syslog "github.com/leodido/go-syslog/v4"
)

// WithBestEffort enables the best effort mode.
func WithBestEffort() syslog.MachineOption {
	return func(m syslog.Machine) syslog.Machine {
		m.WithBestEffort()
		return m
	}
}

// WithYear sets the strategy to decide the year for the Stamp timestamp of RFC 3164.
func WithYear(o YearOperator) syslog.MachineOption {
	return func(m syslog.Machine) syslog.Machine {
		m.(*machine).WithYear(o)
		return m
	}
}

// WithTimezone sets the strategy to decide the timezone to apply to the Stamp timestamp of RFC 3164.
func WithTimezone(loc *time.Location) syslog.MachineOption {
	return func(m syslog.Machine) syslog.Machine {
		m.(*machine).WithTimezone(loc)
		return m
	}
}

// WithLocaleTimezone sets the strategy to decide the timezone to apply to the Stamp timestamp of RFC 3164.
func WithLocaleTimezone(loc *time.Location) syslog.MachineOption {
	return func(m syslog.Machine) syslog.Machine {
		m.(*machine).WithLocaleTimezone(loc)
		return m
	}
}

// todo > WithStrictHostname() option - see RFC3164 page 10
// WithStrictHostname tells the parser to match the hostnames strictly as per RFC 3164 recommentations.
//
// The HOSTNAME field will contain only the hostname, the IPv4 address, or the IPv6 address of the originator of the message.
// The preferred value is the hostname.
// If the hostname is used, the HOSTNAME field MUST contain the hostname of the device as specified in STD 13 [4].
// It should be noted that this MUST NOT contain any embedded spaces.
// The Domain Name MUST NOT be included in the HOSTNAME field.
// If the IPv4 address is used, it MUST be shown as the dotted decimal notation as used in STD 13 [5].
// If an IPv6 address is used, any valid representation used in RFC 2373 [6] MAY be used.
// A single space character MUST also follow the HOSTNAME field.
// func WithStrictHostname() syslog.MachineOption {
// 	return func(m syslog.Machine) syslog.Machine {
// 		m.(*machine).WithStrictHostname()
// 		return m
// 	}
// }

// WithRFC3339 tells the parser to look for RFC3339 timestamps, too.
//
// It tells the parser to accept also RFC3339 timestamps even if they are not in the RFC3164 timestamp part.
// Note that WithYear option will be ignored when an RFC3339 timestamp will match.
func WithRFC3339() syslog.MachineOption {
	return func(m syslog.Machine) syslog.Machine {
		m.(*machine).WithRFC3339()
		return m
	}
}
