package rfc3164

import (
	"time"

	"github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/common"
)

type syslogMessage struct {
	prioritySet      bool // We explictly flag the setting of priority since its zero value is a valid priority by RFC 3164
	timestampSet     bool // We explictly flag the setting of timestamp since its zero value is a valid timestamp by RFC 3164
	msgcountSet      bool
	sequenceSet      bool
	priorityOptional bool
	priority         uint8
	msgcount         uint32
	sequence         uint32
	timestamp        time.Time
	hostname         string
	tag              string
	content          string
	message          string
}

func (sm *syslogMessage) minimal() bool {
	if sm.prioritySet {
		return common.ValidPriority(sm.priority)
	}
	return sm.priorityOptional && sm.timestampSet && sm.message != ""
}

// export is meant to be called on minimally valid messages. PRI-derived fields
// are populated only when PRI was parsed.
func (sm *syslogMessage) export() *SyslogMessage {
	out := &SyslogMessage{}
	if sm.prioritySet {
		out.ComputeFromPriority(sm.priority)
	}

	if sm.msgcountSet {
		out.MessageCounter = &sm.msgcount
	}
	if sm.sequenceSet {
		out.Sequence = &sm.sequence
	}
	if sm.timestampSet {
		out.Timestamp = &sm.timestamp
	}
	if sm.hostname != "-" && sm.hostname != "" {
		out.Hostname = &sm.hostname
	}
	if sm.tag != "-" && sm.tag != "" {
		out.Appname = &sm.tag
	}
	if sm.content != "-" && sm.content != "" {
		// Content is usually process ID
		// See https://tools.ietf.org/html/rfc3164#section-5.3
		out.ProcID = &sm.content
	}
	if sm.message != "" {
		out.Message = &sm.message
	}

	return out
}

// SyslogMessage represents a RFC3164 syslog message.
type SyslogMessage struct {
	syslog.Base
}

// Valid reports whether the message has either a valid PRI or the structural
// fields required for a parsed priorityless RFC3164 message.
func (sm *SyslogMessage) Valid() bool {
	if sm.Priority != nil {
		return common.ValidPriority(*sm.Priority)
	}
	return sm.Timestamp != nil && sm.Message != nil
}
