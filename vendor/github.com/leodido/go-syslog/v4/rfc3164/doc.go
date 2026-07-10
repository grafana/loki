// Package rfc3164 provides a parser for RFC 3164 (BSD syslog) messages.
// The parser is implemented as a Ragel finite-state machine and supports
// extensions for Cisco IOS syslog formats, RFC 3339 timestamps, and
// configurable timezone/year handling.
package rfc3164
