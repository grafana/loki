package rfc5424

import (
	"time"
)

type syslogMessage struct {
	prioritySet    bool // We explictly flag the setting of priority since its zero value is a valid priority by RFC 5424
	timestampSet   bool // We explictly flag the setting of timestamp since its zero value is a valid timestamp by RFC 5424
	hasElements    bool
	priority       uint8
	version        uint16
	timestamp      time.Time
	hostname       string
	appname        string
	procID         string
	msgID          string
	structuredData map[string]map[string]string
	message        string
}

func (sm *syslogMessage) valid() bool {
	if sm.prioritySet && sm.version > 0 && sm.version <= 999 {
		return true
	}

	return false
}

func (sm *syslogMessage) export() *SyslogMessage {
	out := &SyslogMessage{}
	if sm.prioritySet {
		out.setPriority(sm.priority)
	}
	if sm.version > 0 && sm.version <= 999 {
		out.version = sm.version
	}
	if sm.timestampSet {
		out.timestamp = &sm.timestamp
	}
	if sm.hostname != "-" && sm.hostname != "" {
		out.hostname = &sm.hostname
	}
	if sm.appname != "-" && sm.appname != "" {
		out.appname = &sm.appname
	}
	if sm.procID != "-" && sm.procID != "" {
		out.procID = &sm.procID
	}
	if sm.msgID != "-" && sm.msgID != "" {
		out.msgID = &sm.msgID
	}
	if sm.hasElements {
		out.structuredData = &sm.structuredData
	}
	if sm.message != "" {
		out.message = &sm.message
	}

	return out
}

// SyslogMessage represents a syslog message.
type SyslogMessage struct {
	priority       *uint8
	facility       *uint8
	severity       *uint8
	version        uint16 // Grammar mandates that version cannot be 0, so we can use the 0 value of uint16 to signal nil
	timestamp      *time.Time
	hostname       *string
	appname        *string
	procID         *string
	msgID          *string
	structuredData *map[string]map[string]string
	message        *string
}

// Valid tells whether the receiving SyslogMessage is well-formed or not.
//
// A minimally well-formed syslog message contains at least a priority ([1, 191] or 0) and the version (]0, 999]).
func (sm *SyslogMessage) Valid() bool {
	// A nil priority or a 0 version means that the message is not valid
	// Not checking the priority range since it's parser responsibility
	if sm.priority != nil && *sm.priority >= 0 && *sm.priority <= 191 && sm.version > 0 && sm.version <= 999 {
		return true
	}

	return false
}

// Priority returns the syslog priority or nil when not set
func (sm *SyslogMessage) Priority() *uint8 {
	return sm.priority
}

// Version returns the syslog version or nil when not set
func (sm *SyslogMessage) Version() uint16 {
	return sm.version
}
func (sm *SyslogMessage) setPriority(value uint8) {
	sm.priority = &value
	facility := uint8(value / 8)
	severity := uint8(value % 8)
	sm.facility = &facility
	sm.severity = &severity
}

// Facility returns the facility code.
func (sm *SyslogMessage) Facility() *uint8 {
	return sm.facility
}

// Severity returns the severity code.
func (sm *SyslogMessage) Severity() *uint8 {
	return sm.severity
}

// FacilityMessage returns the text message for the current facility value.
func (sm *SyslogMessage) FacilityMessage() *string {
	if sm.facility != nil {
		msg := facilities[*sm.facility]
		return &msg
	}

	return nil
}

// FacilityLevel returns the
func (sm *SyslogMessage) FacilityLevel() *string {
	if sm.facility != nil {
		if msg, ok := facilityKeywords[*sm.facility]; ok {
			return &msg
		}

		// Fallback to facility message
		msg := facilities[*sm.facility]
		return &msg
	}

	return nil
}

// SeverityMessage returns the text message for the current severity value.
func (sm *SyslogMessage) SeverityMessage() *string {
	if sm.severity != nil {
		msg := severityMessages[*sm.severity]
		return &msg
	}

	return nil
}

// SeverityLevel returns the text level for the current severity value.
func (sm *SyslogMessage) SeverityLevel() *string {
	if sm.severity != nil {
		msg := severityLevels[*sm.severity]
		return &msg
	}

	return nil
}

// SeverityShortLevel returns the short text level for the current severity value.
func (sm *SyslogMessage) SeverityShortLevel() *string {
	if sm.severity != nil {
		msg := severityLevelsShort[*sm.severity]
		return &msg
	}

	return nil
}

var severityMessages = map[uint8]string{
	0: "system is unusable",
	1: "action must be taken immediately",
	2: "critical conditions",
	3: "error conditions",
	4: "warning conditions",
	5: "normal but significant condition",
	6: "informational messages",
	7: "debug-level messages",
}

var severityLevels = map[uint8]string{
	0: "emergency",
	1: "alert",
	2: "critical",
	3: "error",
	4: "warning",
	5: "notice",
	6: "informational",
	7: "debug",
}

// As per https://github.com/torvalds/linux/blob/master/tools/include/linux/kern_levels.h and syslog(3)
var severityLevelsShort = map[uint8]string{
	0: "emerg",
	1: "alert",
	2: "crit",
	3: "err",
	4: "warning",
	5: "notice",
	6: "info",
	7: "debug",
}

var facilities = map[uint8]string{
	0:  "kernel messages",
	1:  "user-level messages",
	2:  "mail system",
	3:  "system daemons",
	4:  "security/authorization messages",
	5:  "messages generated internally by syslogd",
	6:  "line printer subsystem",
	7:  "network news subsystem",
	8:  "UUCP subsystem",
	9:  "clock daemon",
	10: "security/authorization messages",
	11: "FTP daemon",
	12: "NTP subsystem",
	13: "log audit",
	14: "log alert",
	15: "clock daemon (note 2)", // (todo) > some sources reporting "scheduling daemon"
	16: "local use 0 (local0)",
	17: "local use 1 (local1)",
	18: "local use 2 (local2)",
	19: "local use 3 (local3)",
	20: "local use 4 (local4)",
	21: "local use 5 (local5)",
	22: "local use 6 (local6)",
	23: "local use 7 (local7)",
}

// As per syslog(3)
var facilityKeywords = map[uint8]string{
	0:  "kern",
	1:  "user",
	2:  "mail",
	3:  "daemon",
	4:  "auth",
	5:  "syslog",
	6:  "lpr",
	7:  "news",
	8:  "uucp",
	10: "authpriv",
	11: "ftp",
	15: "cron",
	16: "local0",
	17: "local1",
	18: "local2",
	19: "local3",
	20: "local4",
	21: "local5",
	22: "local6",
	23: "local7",
}

// Timestamp returns the syslog timestamp or nil when not set
func (sm *SyslogMessage) Timestamp() *time.Time {
	return sm.timestamp
}

// Hostname returns the syslog hostname or nil when not set
func (sm *SyslogMessage) Hostname() *string {
	return sm.hostname
}

// ProcID returns the syslog proc ID or nil when not set
func (sm *SyslogMessage) ProcID() *string {
	return sm.procID
}

// Appname returns the syslog appname or nil when not set
func (sm *SyslogMessage) Appname() *string {
	return sm.appname
}

// MsgID returns the syslog msg ID or nil when not set
func (sm *SyslogMessage) MsgID() *string {
	return sm.msgID
}

// Message returns the syslog message or nil when not set
func (sm *SyslogMessage) Message() *string {
	return sm.message
}

// StructuredData returns the syslog structured data or nil when not set
func (sm *SyslogMessage) StructuredData() *map[string]map[string]string {
	return sm.structuredData
}
