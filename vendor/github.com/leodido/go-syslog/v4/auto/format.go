package auto

import (
	syslog "github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/rfc3164"
	"github.com/leodido/go-syslog/v4/rfc5424"
)

// Format identifies the syslog message format detected by auto-detection.
type Format int

const (
	// FormatUnknown indicates the format could not be determined.
	FormatUnknown Format = iota
	// FormatRFC3164 indicates an RFC 3164 (BSD syslog) message.
	FormatRFC3164
	// FormatRFC5424 indicates an RFC 5424 (IETF syslog) message.
	FormatRFC5424
)

// String returns the string representation of the format.
func (f Format) String() string {
	switch f {
	case FormatUnknown:
		return "unknown"
	case FormatRFC3164:
		return "rfc3164"
	case FormatRFC5424:
		return "rfc5424"
	default:
		return "unknown"
	}
}

// DetectFormat returns the syslog format of a parsed message based on its
// concrete type. Returns FormatUnknown for nil, wrapped, or custom
// syslog.Message implementations — only *rfc3164.SyslogMessage and
// *rfc5424.SyslogMessage are recognized.
func DetectFormat(msg syslog.Message) Format {
	if msg == nil {
		return FormatUnknown
	}
	switch msg.(type) {
	case *rfc3164.SyslogMessage:
		return FormatRFC3164
	case *rfc5424.SyslogMessage:
		return FormatRFC5424
	default:
		return FormatUnknown
	}
}
