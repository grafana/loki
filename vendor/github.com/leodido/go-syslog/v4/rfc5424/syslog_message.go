package rfc5424

import (
	"time"

	"github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/common"
)

type syslogMessage struct {
	prioritySet      bool // We explictly flag the setting of priority since its zero value is a valid priority by RFC 5424
	priorityOptional bool
	timestampSet     bool // We explictly flag the setting of timestamp since its zero value is a valid timestamp by RFC 5424
	hasElements      bool
	priority         uint8
	version          uint16
	timestamp        time.Time
	hostname         string
	appname          string
	procID           string
	msgID            string
	structuredData   map[string]map[string]string
	message          string
}

func (sm *syslogMessage) minimal() bool {
	if !common.ValidVersion(sm.version) {
		return false
	}
	if sm.prioritySet {
		return common.ValidPriority(sm.priority)
	}
	return sm.priorityOptional
}

// export is meant to be called on minimally-valid messages.
func (sm *syslogMessage) export() *SyslogMessage {
	out := &SyslogMessage{}
	if sm.prioritySet {
		out.ComputeFromPriority(sm.priority)
	}
	out.Version = sm.version

	if sm.timestampSet {
		out.Timestamp = &sm.timestamp
	}
	if sm.hostname != "-" && sm.hostname != "" {
		out.Hostname = &sm.hostname
	}
	if sm.appname != "-" && sm.appname != "" {
		out.Appname = &sm.appname
	}
	if sm.procID != "-" && sm.procID != "" {
		out.ProcID = &sm.procID
	}
	if sm.msgID != "-" && sm.msgID != "" {
		out.MsgID = &sm.msgID
	}
	if sm.hasElements {
		out.StructuredData = &sm.structuredData
	}
	if sm.message != "" {
		out.Message = &sm.message
	}

	return out
}

// Builder represents a RFC5424 syslog message builder.
type Builder interface {
	syslog.Message

	SetPriority(value uint8) Builder
	SetVersion(value uint16) Builder
	SetTimestamp(value string) Builder
	SetHostname(value string) Builder
	SetAppname(value string) Builder
	SetProcID(value string) Builder
	SetMsgID(value string) Builder
	SetElementID(value string) Builder
	SetParameter(id string, name string, value string) Builder
	SetMessage(value string) Builder
}

// SyslogMessage represents a RFC5424 syslog message.
//
// A SyslogMessage is not safe for concurrent use by multiple goroutines.
// Callers that need to build or mutate messages from multiple goroutines
// should use a separate SyslogMessage per goroutine, or provide their own synchronization.
type SyslogMessage struct {
	syslog.Base

	Version        uint16 // Grammar mandates that version cannot be 0, so we can use the 0 value of uint16 to signal nil
	StructuredData *map[string]map[string]string
}

// Valid reports parser-level structural validity. A valid VERSION is required;
// PRI may be absent, but when present it must be valid. Valid does not imply
// that String can serialize the message: serialization always requires PRI.
func (sm *SyslogMessage) Valid() bool {
	if !common.ValidVersion(sm.Version) {
		return false
	}
	return sm.Priority == nil || common.ValidPriority(*sm.Priority)
}
