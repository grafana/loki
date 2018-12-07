// Copyright 2017 Google Inc. All Rights Reserved.
// This file is available under the Apache license.

package logline

// LogLine contains all the information about a line just read from a log.
type LogLine struct {
	Filename string // The log filename that this line was read from
	Line     string // The text of the log line itself up to the newline.
}

// NewLogLine creates a new LogLine object.
func NewLogLine(filename string, line string) *LogLine {
	return &LogLine{filename, line}
}
