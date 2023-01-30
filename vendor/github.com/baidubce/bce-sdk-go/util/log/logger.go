/*
 * Copyright 2017 Baidu, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License"); you may not use this file
 * except in compliance with the License. You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under the
 * License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND,
 * either express or implied. See the License for the specific language governing permissions
 * and limitations under the License.
 */

// logger.go - defines the logger structure and methods

// Package log implements the log facilities for BCE. It supports log to stderr, stdout as well as
// log to file with rotating. It is safe to be called by multiple goroutines.
// By using the package level function to use the default logger:
//     log.SetLogHandler(log.STDOUT | log.FILE) // default is log to stdout
//     log.SetLogDir("/tmp")                    // default is /tmp
//     log.SetRotateType(log.ROTATE_DAY)        // default is log.HOUR
//     log.SetRotateSize(1 << 30)               // default is 1GB
//     log.SetLogLevel(log.INFO)                // default is log.DEBUG
//     log.Debug(1, 1.2, "a")
//     log.Debugln(1, 1.2, "a")
//     log.Debugf(1, 1.2, "a")
// User can also create new logger without using the default logger:
//     customLogger := log.NewLogger()
//     customLogger.SetLogHandler(log.FILE)
//     customLogger.Debug(1, 1.2, "a")
// The log format can also support custom setting by using the following interface:
//     log.SetLogFormat([]string{log.FMT_LEVEL, log.FMT_TIME, log.FMT_MSG})
// Most of the cases just use the default format is enough:
//     []string{FMT_LEVEL, FMT_LTIME, FMT_LOCATION, FMT_MSG}
package log

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Handler uint8

// Constants for log handler flags, default is STDOUT
const (
	NONE   Handler = 0
	STDOUT Handler = 1
	STDERR Handler = 1 << 1
	FILE   Handler = 1 << 2
)

type RotateStrategy uint8

// Constants for log rotating strategy when logging to file, default is by hour
const (
	ROTATE_NONE RotateStrategy = iota
	ROTATE_DAY
	ROTATE_HOUR
	ROTATE_MINUTE
	ROTATE_SIZE

	DEFAULT_ROTATE_TYPE           = ROTATE_HOUR
	DEFAULT_ROTATE_SIZE     int64 = 1 << 30
	DEFAULT_LOG_DIR               = "/tmp"
	ROTATE_SIZE_FILE_PREFIX       = "rotating"
)

type Level uint8

// Constants for log levels, default is DEBUG
const (
	DEBUG Level = iota
	INFO
	WARN
	ERROR
	FATAL
	PANIC
)

var gLevelString = [...]string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL", "PANIC"}

// Constants of the log format components to support user custom specification
const (
	FMT_LEVEL    = "level"
	FMT_LTIME    = "ltime"    // long time with microsecond
	FMT_TIME     = "time"     // just with second
	FMT_LOCATION = "location" // caller's location with file, line, function
	FMT_MSG      = "msg"
)

var (
	LOG_FMT_STR = map[string]string{
		FMT_LEVEL:    "[%s]",
		FMT_LTIME:    "2006-01-02 15:04:05.000000",
		FMT_TIME:     "2006-01-02 15:04:05",
		FMT_LOCATION: "%s:%d:%s:",
		FMT_MSG:      "%s",
	}
	gDefaultLogFormat = []string{FMT_LEVEL, FMT_LTIME, FMT_LOCATION, FMT_MSG}
)

type writerArgs struct {
	record     string
	rotateArgs interface{} // used for rotating: the size of the record or the logging time
}

// Logger defines the internal implementation of the log facility
type logger struct {
	writers        map[Handler]io.WriteCloser // the destination writer to log message
	writerChan     chan *writerArgs           // the writer channal to pass each record and time or size
	logFormat      []string
	levelThreshold Level
	handler        Handler

	// Fields that used when logging to file
	logDir     string
	logFile    string
	rotateType RotateStrategy
	rotateSize int64
	done       chan bool
}

func (l *logger) logging(level Level, format string, args ...interface{}) {
	// Only log message that set the handler and is greater than or equal to the threshold
	if l.handler == NONE || level < l.levelThreshold {
		return
	}

	// Generate the log record string and pass it to the writer channel
	now := time.Now()
	pc, file, line, ok, funcname := uintptr(0), "???", 0, true, "???"
	pc, file, line, ok = runtime.Caller(2)
	if ok {
		funcname = runtime.FuncForPC(pc).Name()
		funcname = filepath.Ext(funcname)
		funcname = strings.TrimPrefix(funcname, ".")
		file = filepath.Base(file)
	}
	buf := make([]string, 0, len(l.logFormat))
	msg := fmt.Sprintf(format, args...)
	for _, f := range l.logFormat {
		if _, exists := LOG_FMT_STR[f]; !exists { // skip not supported part
			continue
		}
		fmtStr := LOG_FMT_STR[f]
		switch f {
		case FMT_LEVEL:
			buf = append(buf, fmt.Sprintf(fmtStr, gLevelString[level]))
		case FMT_LTIME:
			buf = append(buf, now.Format(fmtStr))
		case FMT_TIME:
			buf = append(buf, now.Format(fmtStr))
		case FMT_LOCATION:
			buf = append(buf, fmt.Sprintf(fmtStr, file, line, funcname))
		case FMT_MSG:
			buf = append(buf, fmt.Sprintf(fmtStr, msg))
		}
	}
	record := strings.Join(buf, " ")
	if l.rotateType == ROTATE_SIZE {
		l.writerChan <- &writerArgs{record, int64(len(record))}
	} else {
		l.writerChan <- &writerArgs{record, now}
	}

	// wait for current record done logging
}

func (l *logger) buildWriter(args interface{}) {
	if l.handler&STDOUT == STDOUT {
		l.writers[STDOUT] = os.Stdout
	} else {
		delete(l.writers, STDOUT)
	}
	if l.handler&STDERR == STDERR {
		l.writers[STDERR] = os.Stderr
	} else {
		delete(l.writers, STDERR)
	}
	if l.handler&FILE == FILE {
		l.writers[FILE] = l.buildFileWriter(args)
	} else {
		delete(l.writers, FILE)
	}
}

func (l *logger) buildFileWriter(args interface{}) io.WriteCloser {
	if l.handler&FILE != FILE {
		return os.Stderr
	}

	if len(l.logDir) == 0 {
		l.logDir = DEFAULT_LOG_DIR
	}
	if l.rotateType < ROTATE_NONE || l.rotateType > ROTATE_SIZE {
		l.rotateType = DEFAULT_ROTATE_TYPE
	}
	if l.rotateType == ROTATE_SIZE && l.rotateSize == 0 {
		l.rotateSize = DEFAULT_ROTATE_SIZE
	}

	logFile, needCreateFile := "", false
	if l.rotateType == ROTATE_SIZE {
		recordSize, _ := args.(int64)
		logFile, needCreateFile = l.buildFileWriterBySize(recordSize)
	} else {
		recordTime, _ := args.(time.Time)
		switch l.rotateType {
		case ROTATE_NONE:
			logFile = "default.log"
		case ROTATE_DAY:
			logFile = recordTime.Format("2006-01-02.log")
		case ROTATE_HOUR:
			logFile = recordTime.Format("2006-01-02_15.log")
		case ROTATE_MINUTE:
			logFile = recordTime.Format("2006-01-02_15-04.log")
		}
		if _, exist := getFileInfo(filepath.Join(l.logDir, logFile)); !exist {
			needCreateFile = true
		}
	}
	l.logFile = logFile
	logFile = filepath.Join(l.logDir, l.logFile)

	// Should create new file
	if needCreateFile {
		if w, ok := l.writers[FILE]; ok {
			w.Close()
		}
		if writer, err := os.Create(logFile); err == nil {
			return writer
		} else {
			return os.Stderr
		}
	}

	// Already open the file
	if w, ok := l.writers[FILE]; ok {
		return w
	}

	// Newly open the file
	if writer, err := os.OpenFile(logFile, os.O_WRONLY|os.O_APPEND, 0666); err == nil {
		return writer
	} else {
		return os.Stderr
	}
}

func (l *logger) buildFileWriterBySize(recordSize int64) (string, bool) {
	logFile, needCreateFile := "", false
	// First running the program and need to get filename by checking the existed files
	if len(l.logFile) == 0 {
		fname := fmt.Sprintf("%s-%s.0.log", ROTATE_SIZE_FILE_PREFIX, getSizeString(l.rotateSize))
		for {
			size, exist := getFileInfo(filepath.Join(l.logDir, fname))
			if !exist {
				logFile, needCreateFile = fname, true
				break
			}
			if exist && size+recordSize <= l.rotateSize {
				logFile, needCreateFile = fname, false
				break
			}
			fname = getNextFileName(fname)
		}
	} else { // check the file size to append to the existed file or create a new file
		currentFile := filepath.Join(l.logDir, l.logFile)
		size, exist := getFileInfo(currentFile)
		if !exist {
			logFile, needCreateFile = l.logFile, true
		} else {
			if size+recordSize > l.rotateSize { // size exceeded
				logFile, needCreateFile = getNextFileName(l.logFile), true
			} else {
				logFile, needCreateFile = l.logFile, false
			}
		}
	}
	return logFile, needCreateFile
}

func (l *logger) SetHandler(h Handler) { l.handler = h }

func (l *logger) SetLogDir(dir string) { l.logDir = dir }

func (l *logger) SetLogLevel(level Level) { l.levelThreshold = level }

func (l *logger) SetLogFormat(format []string) { l.logFormat = format }

func (l *logger) SetRotateType(rotate RotateStrategy) { l.rotateType = rotate }

func (l *logger) SetRotateSize(size int64) { l.rotateSize = size }

func (l *logger) Debug(msg ...interface{}) { l.logging(DEBUG, "%s\n", concat(msg...)) }

func (l *logger) Debugln(msg ...interface{}) { l.logging(DEBUG, "%s\n", concat(msg...)) }

func (l *logger) Debugf(f string, msg ...interface{}) { l.logging(DEBUG, f+"\n", msg...) }

func (l *logger) Info(msg ...interface{}) { l.logging(INFO, "%s\n", concat(msg...)) }

func (l *logger) Infoln(msg ...interface{}) { l.logging(INFO, "%s\n", concat(msg...)) }

func (l *logger) Infof(f string, msg ...interface{}) { l.logging(INFO, f+"\n", msg...) }

func (l *logger) Warn(msg ...interface{}) { l.logging(WARN, "%s\n", concat(msg...)) }

func (l *logger) Warnln(msg ...interface{}) { l.logging(WARN, "%s\n", concat(msg...)) }

func (l *logger) Warnf(f string, msg ...interface{}) { l.logging(WARN, f+"\n", msg...) }

func (l *logger) Error(msg ...interface{}) { l.logging(ERROR, "%s\n", concat(msg...)) }

func (l *logger) Errorln(msg ...interface{}) { l.logging(ERROR, "%s\n", concat(msg...)) }

func (l *logger) Errorf(f string, msg ...interface{}) { l.logging(ERROR, f+"\n", msg...) }

func (l *logger) Fatal(msg ...interface{}) { l.logging(FATAL, "%s\n", concat(msg...)) }

func (l *logger) Fatalln(msg ...interface{}) { l.logging(FATAL, "%s\n", concat(msg...)) }

func (l *logger) Fatalf(f string, msg ...interface{}) { l.logging(FATAL, f+"\n", msg...) }

func (l *logger) Panic(msg ...interface{}) {
	record := concat(msg...)
	l.logging(PANIC, "%s\n", record)
	panic(record)
}

func (l *logger) Panicln(msg ...interface{}) {
	record := concat(msg...)
	l.logging(PANIC, "%s\n", record)
	panic(record)
}

func (l *logger) Panicf(format string, msg ...interface{}) {
	record := fmt.Sprintf(format, msg...)
	l.logging(PANIC, format+"\n", msg...)
	panic(record)
}

func (l *logger) Close() {
	select {
	case <-l.done:
		return
	default:
	}
	l.writerChan <- nil
}

func NewLogger() *logger {
	obj := &logger{
		writers:        make(map[Handler]io.WriteCloser, 3), // now only support 3 kinds of handler
		writerChan:     make(chan *writerArgs, 100),
		logFormat:      gDefaultLogFormat,
		levelThreshold: DEBUG,
		handler:        NONE,
		done:           make(chan bool),
	}
	// The backend writer goroutine to write each log record
	go func() {
		defer func() {
			if e := recover(); e != nil {
				fmt.Println(e)
			}
		}()
		for {
			select {
			case <-obj.done:
				return
			case args := <-obj.writerChan: // wait until a record comes to log
				if args == nil {
					close(obj.done)
					close(obj.writerChan)
					return
				}
				obj.buildWriter(args.rotateArgs)
				for _, w := range obj.writers {
					fmt.Fprint(w, args.record)
				}
			}
		}
	}()

	return obj
}
