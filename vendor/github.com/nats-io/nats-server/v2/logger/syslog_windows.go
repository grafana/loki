// Copyright 2012-2018 The NATS Authors
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

// Package logger logs to the windows event log
package logger

import (
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/svc/eventlog"
)

var natsEventSource = "NATS-Server"

// SetSyslogName sets the name to use for the system log event source
func SetSyslogName(name string) {
	natsEventSource = name
}

// SysLogger logs to the windows event logger
type SysLogger struct {
	writer *eventlog.Log
	debug  bool
	trace  bool
}

// NewSysLogger creates a log using the windows event logger
func NewSysLogger(debug, trace bool) *SysLogger {
	if err := eventlog.InstallAsEventCreate(natsEventSource, eventlog.Info|eventlog.Error|eventlog.Warning); err != nil {
		if !strings.Contains(err.Error(), "registry key already exists") {
			panic(fmt.Sprintf("could not access event log: %v", err))
		}
	}

	w, err := eventlog.Open(natsEventSource)
	if err != nil {
		panic(fmt.Sprintf("could not open event log: %v", err))
	}

	return &SysLogger{
		writer: w,
		debug:  debug,
		trace:  trace,
	}
}

// NewRemoteSysLogger creates a remote event logger
func NewRemoteSysLogger(fqn string, debug, trace bool) *SysLogger {
	w, err := eventlog.OpenRemote(fqn, natsEventSource)
	if err != nil {
		panic(fmt.Sprintf("could not open event log: %v", err))
	}

	return &SysLogger{
		writer: w,
		debug:  debug,
		trace:  trace,
	}
}

func formatMsg(tag, format string, v ...interface{}) string {
	orig := fmt.Sprintf(format, v...)
	return fmt.Sprintf("pid[%d][%s]: %s", os.Getpid(), tag, orig)
}

// Noticef logs a notice statement
func (l *SysLogger) Noticef(format string, v ...interface{}) {
	l.writer.Info(1, formatMsg("NOTICE", format, v...))
}

// Warnf logs a warning statement
func (l *SysLogger) Warnf(format string, v ...interface{}) {
	l.writer.Info(1, formatMsg("WARN", format, v...))
}

// Fatalf logs a fatal error
func (l *SysLogger) Fatalf(format string, v ...interface{}) {
	msg := formatMsg("FATAL", format, v...)
	l.writer.Error(5, msg)
	panic(msg)
}

// Errorf logs an error statement
func (l *SysLogger) Errorf(format string, v ...interface{}) {
	l.writer.Error(2, formatMsg("ERROR", format, v...))
}

// Debugf logs a debug statement
func (l *SysLogger) Debugf(format string, v ...interface{}) {
	if l.debug {
		l.writer.Info(3, formatMsg("DEBUG", format, v...))
	}
}

// Tracef logs a trace statement
func (l *SysLogger) Tracef(format string, v ...interface{}) {
	if l.trace {
		l.writer.Info(4, formatMsg("TRACE", format, v...))
	}
}
