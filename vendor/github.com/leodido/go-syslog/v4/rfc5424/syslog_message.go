package rfc5424

import (
	"time"

	"github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/common"
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

func (sm *syslogMessage) minimal() bool {
	return sm.prioritySet && common.ValidPriority(sm.priority) && common.ValidVersion(sm.version)
}

// export is meant to be called on minimally-valid messages
// thus it presumes priority and version values exists and are correct
func (sm *syslogMessage) export() *SyslogMessage {
	out := &SyslogMessage{}
	out.ComputeFromPriority(sm.priority)
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
type SyslogMessage struct {
	syslog.Base

	Version        uint16 // Grammar mandates that version cannot be 0, so we can use the 0 value of uint16 to signal nil
	StructuredData *map[string]map[string]string
}

// Valid tells whether the receiving RFC5424 SyslogMessage is well-formed or not.
//
// A minimally well-formed RFC5424 syslog message contains at least a priority ([1, 191] or 0) and the version (]0, 999]).
func (sm *SyslogMessage) Valid() bool {
	// A nil priority or a 0 version means that the message is not valid
	return sm.Base.Valid() && common.ValidVersion(sm.Version)
}
