// Package rfc5424 provides a parser and builder for RFC 5424 (IETF syslog)
// messages. The parser is implemented as a Ragel finite-state machine for
// performance. PRI is required by default; WithOptionalPriority enables
// parser-only compatibility with senders that omit it. The builder always
// requires PRI when serializing.
package rfc5424
