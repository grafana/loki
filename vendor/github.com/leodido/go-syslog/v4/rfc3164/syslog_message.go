package rfc3164

import (
	"time"

	"github.com/leodido/go-syslog/v4"
	"github.com/leodido/go-syslog/v4/common"
)

type syslogMessage struct {
	prioritySet  bool // We explictly flag the setting of priority since its zero value is a valid priority by RFC 3164
	timestampSet bool // We explictly flag the setting of timestamp since its zero value is a valid timestamp by RFC 3164
	msgcountSet  bool
	sequenceSet  bool
	priority     uint8
	msgcount     uint32
	sequence     uint32
	timestamp    time.Time
	hostname     string
	tag          string
	content      string
	message      string
}

func (sm *syslogMessage) minimal() bool {
	return sm.prioritySet && common.ValidPriority(sm.priority)
}

// export is meant to be called on minimally-valid messages
// thus it presumes priority and version values exists and are correct
func (sm *syslogMessage) export() *SyslogMessage {
	out := &SyslogMessage{}
	out.ComputeFromPriority(sm.priority)

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
