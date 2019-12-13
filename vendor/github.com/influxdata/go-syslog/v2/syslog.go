// Package syslog provides generic interfaces and structs for syslog messages and transport.
// Subpackages contains various parsers or scanners for different syslog formats.
package syslog

import (
	"io"
	"time"
)

// BestEfforter is an interface that wraps the HasBestEffort method.
type BestEfforter interface {
	WithBestEffort()
	HasBestEffort() bool
}

// Machine represent a FSM able to parse an entire syslog message and return it in an structured way.
type Machine interface {
	Parse(input []byte) (Message, error)
	BestEfforter
}

// MachineOption represents the type of option setters for Machine instances.
type MachineOption func(m Machine) Machine

// Parser is an interface that wraps the Parse method.
type Parser interface {
	Parse(r io.Reader)
	WithListener(ParserListener)
	BestEfforter
}

// ParserOption represent the type of option setters for Parser instances.
type ParserOption func(p Parser) Parser

// ParserListener is a function that receives syslog parsing results, one by one.
type ParserListener func(*Result)

// Result wraps the outcomes obtained parsing a syslog message.
type Result struct {
	Message Message
	Error   error
}

// Message represent a structured representation of a syslog message.
type Message interface {
	Valid() bool
	Priority() *uint8
	Version() uint16
	Facility() *uint8
	Severity() *uint8
	FacilityMessage() *string
	FacilityLevel() *string
	SeverityMessage() *string
	SeverityLevel() *string
	SeverityShortLevel() *string
	Timestamp() *time.Time
	Hostname() *string
	ProcID() *string
	Appname() *string
	MsgID() *string
	Message() *string
	StructuredData() *map[string]map[string]string
}
