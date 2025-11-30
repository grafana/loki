/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */
/*
 * Content before git sha 34fdeebefcbf183ed7f916f931aa0586fdaa1b40
 * Copyright (c) 2016, The Gocql authors,
 * provided under the BSD-3-Clause License.
 * See the NOTICE file distributed with this work for additional information.
 */

package gocql

import (
	"bytes"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
)

// Deprecated: use StructuredLogger instead
type StdLogger interface{}

func logHelper(logger StructuredLogger, level LogLevel, msg string, fields ...LogField) {
	switch level {
	case LogLevelDebug:
		logger.Debug(msg, fields...)
	case LogLevelInfo:
		logger.Info(msg, fields...)
	case LogLevelWarn:
		logger.Warning(msg, fields...)
	case LogLevelError:
		logger.Error(msg, fields...)
	default:
		logger.Error("Unknown log level", newLogFieldInt("level", int(level)), newLogFieldString("msg", msg))
	}
}

type nopLogger struct{}

func (n nopLogger) Error(_ string, _ ...LogField) {}

func (n nopLogger) Warning(_ string, _ ...LogField) {}

func (n nopLogger) Info(_ string, _ ...LogField) {}

func (n nopLogger) Debug(_ string, _ ...LogField) {}

var nopLoggerSingleton = &nopLogger{}

type testLogger struct {
	logLevel LogLevel
	capture  bytes.Buffer
	mu       sync.Mutex
}

func newTestLogger(logLevel LogLevel) *testLogger {
	return &testLogger{logLevel: logLevel}
}

func (l *testLogger) Error(msg string, fields ...LogField) {
	if LogLevelError <= l.logLevel {
		l.write("ERR gocql: ", msg, fields)
	}
}

func (l *testLogger) Warning(msg string, fields ...LogField) {
	if LogLevelWarn <= l.logLevel {
		l.write("WRN gocql: ", msg, fields)
	}
}

func (l *testLogger) Info(msg string, fields ...LogField) {
	if LogLevelInfo <= l.logLevel {
		l.write("INF gocql: ", msg, fields)
	}
}

func (l *testLogger) Debug(msg string, fields ...LogField) {
	if LogLevelDebug <= l.logLevel {
		l.write("DBG gocql: ", msg, fields)
	}
}

func (l *testLogger) write(prefix string, msg string, fields []LogField) {
	buf := bytes.Buffer{}
	writeLogMsg(&buf, prefix, msg, fields)
	l.mu.Lock()
	defer l.mu.Unlock()
	l.capture.WriteString(buf.String() + "\n")
}

func (l *testLogger) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.capture.String()
}

type defaultLogger struct {
	logLevel LogLevel
}

// NewLogger creates a StructuredLogger that uses the standard library log package.
//
// This logger will write log messages in the following format:
//
//	<LOG_LEVEL> gocql: <message> <fields[0].Name>=<fields[0].Value> <fields[1].Name>=<fields[1].Value>
//
// LOG_LEVEL is always a 3 letter string:
//   - DEBUG -> DBG
//   - INFO -> INF
//   - WARNING -> WRN
//   - ERROR -> ERR
//
// Example:
//
//	INF gocql: Adding host (session initialization). host_addr=127.0.0.1 host_id=a21dd06e-9e7e-4528-8ad7-039604e25e73
func NewLogger(logLevel LogLevel) StructuredLogger {
	return &defaultLogger{logLevel: logLevel}
}

func (l *defaultLogger) Error(msg string, fields ...LogField) {
	if LogLevelError <= l.logLevel {
		l.write("ERR gocql: ", msg, fields)
	}
}

func (l *defaultLogger) Warning(msg string, fields ...LogField) {
	if LogLevelWarn <= l.logLevel {
		l.write("WRN gocql: ", msg, fields)
	}
}

func (l *defaultLogger) Info(msg string, fields ...LogField) {
	if LogLevelInfo <= l.logLevel {
		l.write("INF gocql: ", msg, fields)
	}
}

func (l *defaultLogger) Debug(msg string, fields ...LogField) {
	if LogLevelDebug <= l.logLevel {
		l.write("DBG gocql: ", msg, fields)
	}
}

func (l *defaultLogger) write(prefix string, msg string, fields []LogField) {
	buf := bytes.Buffer{}
	writeLogMsg(&buf, prefix, msg, fields)
	log.Println(buf.String())
}

func writeFields(buf *bytes.Buffer, fields []LogField) {
	for i, field := range fields {
		if i > 0 {
			buf.WriteRune(' ')
		}
		buf.WriteString(field.Name)
		buf.WriteRune('=')
		buf.WriteString(field.Value.String())
	}
}

func writeLogMsg(buf *bytes.Buffer, prefix string, msg string, fields []LogField) {
	buf.WriteString(prefix)
	buf.WriteString(msg)
	buf.WriteRune(' ')
	writeFields(buf, fields)
}

// LogLevel represents the level of logging to be performed.
// Higher values indicate more verbose logging.
// Available levels: LogLevelDebug, LogLevelInfo, LogLevelWarn, LogLevelError, LogLevelNone.
type LogLevel int

const (
	LogLevelDebug = LogLevel(5)
	LogLevelInfo  = LogLevel(4)
	LogLevelWarn  = LogLevel(3)
	LogLevelError = LogLevel(2)
	LogLevelNone  = LogLevel(0)
)

func (recv LogLevel) String() string {
	switch recv {
	case LogLevelDebug:
		return "debug"
	case LogLevelInfo:
		return "info"
	case LogLevelWarn:
		return "warn"
	case LogLevelError:
		return "error"
	case LogLevelNone:
		return "none"
	default:
		// fmt.sprintf allocates so use strings.Join instead
		temp := [2]string{"invalid level ", strconv.Itoa(int(recv))}
		return strings.Join(temp[:], "")
	}
}

// LogField represents a structured log field with a name and value.
// It is used to provide structured logging information.
type LogField struct {
	Name  string
	Value LogFieldValue
}

func newLogField(name string, value LogFieldValue) LogField {
	return LogField{
		Name:  name,
		Value: value,
	}
}

func newLogFieldIp(name string, value net.IP) LogField {
	var str string
	if value == nil {
		str = "<nil>"
	} else {
		str = value.String()
	}
	return newLogField(name, logFieldValueString(str))
}

func newLogFieldError(name string, value error) LogField {
	var str string
	if value != nil {
		str = value.Error()
	}
	return newLogField(name, logFieldValueString(str))
}

func newLogFieldStringer(name string, value fmt.Stringer) LogField {
	var str string
	if value != nil {
		str = value.String()
	}
	return newLogField(name, logFieldValueString(str))
}

func newLogFieldString(name string, value string) LogField {
	return newLogField(name, logFieldValueString(value))
}

func newLogFieldInt(name string, value int) LogField {
	return newLogField(name, logFieldValueInt64(int64(value)))
}

func newLogFieldBool(name string, value bool) LogField {
	return newLogField(name, logFieldValueBool(value))
}

type StructuredLogger interface {
	Error(msg string, fields ...LogField)
	Warning(msg string, fields ...LogField)
	Info(msg string, fields ...LogField)
	Debug(msg string, fields ...LogField)
}

// A LogFieldValue can represent any Go value, but unlike type any,
// it can represent most small values without an allocation.
// The zero Value corresponds to nil.
type LogFieldValue struct {
	num uint64
	any interface{}
}

// LogFieldValueType represents the type of a LogFieldValue.
// It is used to determine how to interpret the value stored in LogFieldValue.
// Available types: LogFieldTypeAny, LogFieldTypeBool, LogFieldTypeInt64, LogFieldTypeString.
type LogFieldValueType int

// It's important that LogFieldTypeAny is 0 so that a zero Value represents nil.
const (
	LogFieldTypeAny LogFieldValueType = iota
	LogFieldTypeBool
	LogFieldTypeInt64
	LogFieldTypeString
)

// LogFieldValueType returns v's LogFieldValueType.
func (v LogFieldValue) LogFieldValueType() LogFieldValueType {
	switch x := v.any.(type) {
	case LogFieldValueType:
		return x
	case string:
		return LogFieldTypeString
	default:
		return LogFieldTypeAny
	}
}

func logFieldValueString(value string) LogFieldValue {
	return LogFieldValue{any: value}
}

func logFieldValueInt(v int) LogFieldValue {
	return logFieldValueInt64(int64(v))
}

func logFieldValueInt64(v int64) LogFieldValue {
	return LogFieldValue{num: uint64(v), any: LogFieldTypeInt64}
}

func logFieldValueBool(v bool) LogFieldValue {
	u := uint64(0)
	if v {
		u = 1
	}
	return LogFieldValue{num: u, any: LogFieldTypeBool}
}

// Any returns v's value as an interface.
func (v LogFieldValue) Any() interface{} {
	switch v.LogFieldValueType() {
	case LogFieldTypeAny:
		if k, ok := v.any.(LogFieldValueType); ok {
			return k
		}
		return v.any
	case LogFieldTypeInt64:
		return int64(v.num)
	case LogFieldTypeString:
		return v.str()
	case LogFieldTypeBool:
		return v.bool()
	default:
		panic(fmt.Sprintf("bad value type: %s", v.LogFieldValueType()))
	}
}

// String returns LogFieldValue's value as a string, formatted like fmt.Sprint.
//
// Unlike the methods Int64 and Bool which panic if v is of the
// wrong LogFieldValueType, String never panics
// (i.e. it can be called for any LogFieldValueType, not just LogFieldTypeString)
func (v LogFieldValue) String() string {
	return v.stringValue()
}

func (v LogFieldValue) str() string {
	return v.any.(string)
}

// Int64 returns v's value as an int64. It panics
// if v is not a signed integer.
func (v LogFieldValue) Int64() int64 {
	if g, w := v.LogFieldValueType(), LogFieldTypeInt64; g != w {
		panic(fmt.Sprintf("Value type is %s, not %s", g, w))
	}
	return int64(v.num)
}

// Bool returns v's value as a bool. It panics
// if v is not a bool.
func (v LogFieldValue) Bool() bool {
	if g, w := v.LogFieldValueType(), LogFieldTypeBool; g != w {
		panic(fmt.Sprintf("Value type is %s, not %s", g, w))
	}
	return v.bool()
}

func (v LogFieldValue) bool() bool {
	return v.num == 1
}

// stringValue returns a text representation of v.
// v is formatted as with fmt.Sprint.
func (v LogFieldValue) stringValue() string {
	switch v.LogFieldValueType() {
	case LogFieldTypeString:
		return v.str()
	case LogFieldTypeInt64:
		return strconv.FormatInt(int64(v.num), 10)
	case LogFieldTypeBool:
		return strconv.FormatBool(v.bool())
	case LogFieldTypeAny:
		return fmt.Sprint(v.any)
	default:
		panic(fmt.Sprintf("bad value type: %s", v.LogFieldValueType()))
	}
}

var logFieldValueTypeStrings = []string{
	"Any",
	"Bool",
	"Int64",
	"String",
}

func (t LogFieldValueType) String() string {
	if t >= 0 && int(t) < len(logFieldValueTypeStrings) {
		return logFieldValueTypeStrings[t]
	}
	return "<unknown gocql.LogFieldValueType>"
}
