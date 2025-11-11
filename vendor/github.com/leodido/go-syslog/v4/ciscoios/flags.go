// Package ciscoios provides component flags for configuring Cisco IOS syslog parsing.
//
// Cisco IOS devices send syslog messages with optional prepended fields in a fixed order:
//
//	<PRI> [message_counter:] [sequence:] [hostname:] [*]timestamp: message
//
// Each component can be independently enabled or disabled on the Cisco device,
// and the parser must be configured to match the device's configuration.
//
// See rfc3164.WithCiscoIOSComponents() documentation in the rfc3164 package for detailed
// usage instructions and Cisco device configuration examples.
package ciscoios

type Component int

const (
	All                    Component = 0
	DisableMessageCounter  Component = 0x01
	DisableSequenceNumber  Component = 0x02
	DisableHostname        Component = 0x04
	DisableSecondFractions Component = 0x08
)
