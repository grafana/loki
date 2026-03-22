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

// util.go - define the package-level default logger and functions for easily use.

package log

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// The global default logger object to perform logging in this package-level functions
var gDefaultLogger *logger

func init() {
	gDefaultLogger = NewLogger()
}

// Helper functions
func getFileInfo(path string) (int64, bool) {
	info, err := os.Stat(path)
	if err != nil {
		return -1, false
	} else {
		return info.Size(), true
	}
}

func getSizeString(size int64) string {
	if size < 0 {
		return fmt.Sprintf("%d", size)
	}
	if size < (1 << 10) {
		return fmt.Sprintf("%dB", size)
	} else if size < (1 << 20) {
		return fmt.Sprintf("%dkB", size>>10)
	} else if size < (1 << 30) {
		return fmt.Sprintf("%dMB", size>>20)
	} else if size < (1 << 40) {
		return fmt.Sprintf("%dGB", size>>30)
	} else {
		return "SUPER-LARGE"
	}
}

func getNextFileName(nowFileName string) string {
	pos1 := strings.Index(nowFileName, ".")
	rest := nowFileName[pos1+1:]
	pos2 := strings.Index(rest, ".")
	num, _ := strconv.Atoi(rest[0:pos2])
	return fmt.Sprintf("%s.%d.log", nowFileName[0:pos1], num+1)
}

func concat(msg ...interface{}) string {
	buf := make([]string, 0, len(msg))
	for _, m := range msg {
		buf = append(buf, fmt.Sprintf("%v", m))
	}
	return strings.Join(buf, " ")
}

// Export package-level functions for logging messages by using the default logger. It supports 3
// kinds of interface which is similar to the standard package "fmt": with suffix of "ln", "f" or
// nothing.
func Debug(msg ...interface{}) {
	gDefaultLogger.logging(DEBUG, "%s\n", concat(msg...))
}

func Debugln(msg ...interface{}) {
	gDefaultLogger.logging(DEBUG, "%s\n", concat(msg...))
}

func Debugf(format string, msg ...interface{}) {
	gDefaultLogger.logging(DEBUG, format+"\n", msg...)
}

func Info(msg ...interface{}) {
	gDefaultLogger.logging(INFO, "%s\n", concat(msg...))
}

func Infoln(msg ...interface{}) {
	gDefaultLogger.logging(INFO, "%s\n", concat(msg...))
}

func Infof(format string, msg ...interface{}) {
	gDefaultLogger.logging(INFO, format+"\n", msg...)
}

func Warn(msg ...interface{}) {
	gDefaultLogger.logging(WARN, "%s\n", concat(msg...))
}

func Warnln(msg ...interface{}) {
	gDefaultLogger.logging(WARN, "%s\n", concat(msg...))
}

func Warnf(format string, msg ...interface{}) {
	gDefaultLogger.logging(WARN, format+"\n", msg...)
}

func Error(msg ...interface{}) {
	gDefaultLogger.logging(ERROR, "%s\n", concat(msg...))
}

func Errorln(msg ...interface{}) {
	gDefaultLogger.logging(ERROR, "%s\n", concat(msg...))
}

func Errorf(format string, msg ...interface{}) {
	gDefaultLogger.logging(ERROR, format+"\n", msg...)
}

func Fatal(msg ...interface{}) {
	gDefaultLogger.logging(FATAL, "%s\n", concat(msg...))
}

func Fatalln(msg ...interface{}) {
	gDefaultLogger.logging(FATAL, "%s\n", concat(msg...))
}

func Fatalf(format string, msg ...interface{}) {
	gDefaultLogger.logging(FATAL, format+"\n", msg...)
}

func Panic(msg ...interface{}) {
	record := concat(msg...)
	gDefaultLogger.logging(PANIC, "%s\n", record)
	panic(record)
}

func Panicln(msg ...interface{}) {
	record := concat(msg...)
	gDefaultLogger.logging(PANIC, "%s\n", record)
	panic(record)
}

func Panicf(format string, msg ...interface{}) {
	record := fmt.Sprintf(format, msg...)
	gDefaultLogger.logging(PANIC, format+"\n", msg...)
	panic(record)
}

func Close() {
	gDefaultLogger.Close()
}

// SetLogHandler - set the handler of the logger
//
// PARAMS:
//     - Handler: the handler defined in this package, now just support STDOUT, STDERR, FILE
func SetLogHandler(h Handler) {
	gDefaultLogger.handler = h
}

// SetLogLevel - set the level threshold of the logger, only level equal to or bigger than this
// value will be logged.
//
// PARAMS:
//     - Level: the level defined in this package, now support 6 levels.
func SetLogLevel(l Level) {
	gDefaultLogger.levelThreshold = l
}

// SetLogFormat - set the log component of each record when logging it. The default log format is
// {FMT_LEVEL, FMT_LTIME, FMT_LOCATION, FMT_MSG}.
//
// PARAMS:
//     - format: the format component array.
func SetLogFormat(format []string) {
	gDefaultLogger.logFormat = format
}

// SetLogDir - set the logging directory if logging to file.
//
// PARAMS:
//     - dir: the logging directory
// RETURNS:
//     - error: check the directory and try to make it, otherwise return the error.
func SetLogDir(dir string) error {
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			if mkErr := os.Mkdir(dir, os.ModePerm); mkErr != nil {
				return mkErr
			}
		} else {
			return err
		}
	}
	gDefaultLogger.logDir = dir
	return nil
}

// SetRotateType - set the rotating strategy if logging to file.
//
// PARAMS:
//     - RotateStrategy: the rotate strategy defined in this package, now support 5 strategy.
func SetRotateType(r RotateStrategy) {
	gDefaultLogger.rotateType = r
}

// SetRotateSize - set the rotating size if logging to file and set the strategy of size.
//
// PARAMS:
//     - size: the rotating size
// RETURNS:
//     - error: check the value and return any error if error occurs.
func SetRotateSize(size int64) error {
	if size <= 0 {
		return fmt.Errorf("%s", "rotate size should not be negative")
	}
	gDefaultLogger.rotateSize = size
	return nil
}
