// Package syslog provides generic interfaces and structs for syslog messages and transport.
// Subpackages contains various parsers or scanners for different syslog formats.
package syslog

import (
	"io"
	"time"

	"github.com/influxdata/go-syslog/v3/common"
)

// BestEfforter is an interface that wraps the HasBestEffort method.
type BestEfforter interface {
	WithBestEffort()
	HasBestEffort() bool
}

// MaxMessager sets the max message size the parser should be able to parse
type MaxMessager interface {
	WithMaxMessageLength(length int)
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
	MaxMessager
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

// Message represent a minimal syslog message.
type Message interface {
	Valid() bool
	FacilityMessage() *string
	FacilityLevel() *string
	SeverityMessage() *string
	SeverityLevel() *string
	SeverityShortLevel() *string

	ComputeFromPriority(value uint8)
}

// Base represents a base struct for syslog messages.
//
// It contains the fields in common among different formats.
type Base struct {
	Facility  *uint8
	Severity  *uint8
	Priority  *uint8
	Timestamp *time.Time
	Hostname  *string
	Appname   *string
	ProcID    *string
	MsgID     *string
	Message   *string
}

// Valid tells whether the receiving message is well-formed or not.
//
// A minimally well-formed RFC3164 syslog message contains at least the priority ([1, 191] or 0).
// A minimally well-formed RFC5424 syslog message also contains the version.
func (m *Base) Valid() bool {
	// A nil priority or a 0 version means that the message is not valid
	return m.Priority != nil && common.ValidPriority(*m.Priority)
}

// ComputeFromPriority set the priority values and computes facility and severity from it.
//
// It does NOT check the input value validity.
func (m *Base) ComputeFromPriority(value uint8) {
	m.Priority = &value
	facility := uint8(value / 8)
	severity := uint8(value % 8)
	m.Facility = &facility
	m.Severity = &severity
}

// FacilityMessage returns the text message for the current facility value.
func (m *Base) FacilityMessage() *string {
	if m.Facility != nil {
		msg := common.Facility[*m.Facility]
		return &msg
	}

	return nil
}

// FacilityLevel returns the
func (m *Base) FacilityLevel() *string {
	if m.Facility != nil {
		if msg, ok := common.FacilityKeywords[*m.Facility]; ok {
			return &msg
		}

		// Fallback to facility message
		msg := common.Facility[*m.Facility]
		return &msg
	}

	return nil
}

// SeverityMessage returns the text message for the current severity value.
func (m *Base) SeverityMessage() *string {
	if m.Severity != nil {
		msg := common.SeverityMessages[*m.Severity]
		return &msg
	}

	return nil
}

// SeverityLevel returns the text level for the current severity value.
func (m *Base) SeverityLevel() *string {
	if m.Severity != nil {
		msg := common.SeverityLevels[*m.Severity]
		return &msg
	}

	return nil
}

// SeverityShortLevel returns the short text level for the current severity value.
func (m *Base) SeverityShortLevel() *string {
	if m.Severity != nil {
		msg := common.SeverityLevelsShort[*m.Severity]
		return &msg
	}

	return nil
}
