// Copyright 2012-2019 The NATS Authors
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

//go:build !windows
// +build !windows

package logger

import (
	"fmt"
	"log"
	"log/syslog"
	"net/url"
	"os"
	"strings"
)

// SysLogger provides a system logger facility
type SysLogger struct {
	writer *syslog.Writer
	debug  bool
	trace  bool
}

// SetSyslogName sets the name to use for the syslog.
// Currently used only on Windows.
func SetSyslogName(name string) {}

// GetSysLoggerTag generates the tag name for use in syslog statements. If
// the executable is linked, the name of the link will be used as the tag,
// otherwise, the name of the executable is used.  "nats-server" is the default
// for the NATS server.
func GetSysLoggerTag() string {
	procName := os.Args[0]
	if strings.ContainsRune(procName, os.PathSeparator) {
		parts := strings.FieldsFunc(procName, func(c rune) bool {
			return c == os.PathSeparator
		})
		procName = parts[len(parts)-1]
	}
	return procName
}

// NewSysLogger creates a new system logger
func NewSysLogger(debug, trace bool) *SysLogger {
	w, err := syslog.New(syslog.LOG_DAEMON|syslog.LOG_NOTICE, GetSysLoggerTag())
	if err != nil {
		log.Fatalf("error connecting to syslog: %q", err.Error())
	}

	return &SysLogger{
		writer: w,
		debug:  debug,
		trace:  trace,
	}
}

// NewRemoteSysLogger creates a new remote system logger
func NewRemoteSysLogger(fqn string, debug, trace bool) *SysLogger {
	network, addr := getNetworkAndAddr(fqn)
	w, err := syslog.Dial(network, addr, syslog.LOG_DEBUG, GetSysLoggerTag())
	if err != nil {
		log.Fatalf("error connecting to syslog: %q", err.Error())
	}

	return &SysLogger{
		writer: w,
		debug:  debug,
		trace:  trace,
	}
}

func getNetworkAndAddr(fqn string) (network, addr string) {
	u, err := url.Parse(fqn)
	if err != nil {
		log.Fatal(err)
	}

	network = u.Scheme
	if network == "udp" || network == "tcp" {
		addr = u.Host
	} else if network == "unix" {
		addr = u.Path
	} else {
		log.Fatalf("error invalid network type: %q", u.Scheme)
	}

	return
}

// Noticef logs a notice statement
func (l *SysLogger) Noticef(format string, v ...interface{}) {
	l.writer.Notice(fmt.Sprintf(format, v...))
}

// Warnf logs a warning statement
func (l *SysLogger) Warnf(format string, v ...interface{}) {
	l.writer.Warning(fmt.Sprintf(format, v...))
}

// Fatalf logs a fatal error
func (l *SysLogger) Fatalf(format string, v ...interface{}) {
	l.writer.Crit(fmt.Sprintf(format, v...))
}

// Errorf logs an error statement
func (l *SysLogger) Errorf(format string, v ...interface{}) {
	l.writer.Err(fmt.Sprintf(format, v...))
}

// Debugf logs a debug statement
func (l *SysLogger) Debugf(format string, v ...interface{}) {
	if l.debug {
		l.writer.Debug(fmt.Sprintf(format, v...))
	}
}

// Tracef logs a trace statement
func (l *SysLogger) Tracef(format string, v ...interface{}) {
	if l.trace {
		l.writer.Notice(fmt.Sprintf(format, v...))
	}
}
