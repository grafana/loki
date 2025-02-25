// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package configtelemetry // import "go.opentelemetry.io/collector/config/configtelemetry"

import (
	"errors"
	"fmt"
	"strings"
)

const (
	// LevelNone indicates that no telemetry data should be collected.
	LevelNone Level = iota - 1
	// LevelBasic is the recommended and covers the basics of the service telemetry.
	LevelBasic
	// LevelNormal adds some other indicators on top of basic.
	LevelNormal
	// LevelDetailed adds dimensions and views to the previous levels.
	LevelDetailed

	levelNoneStr     = "None"
	levelBasicStr    = "Basic"
	levelNormalStr   = "Normal"
	levelDetailedStr = "Detailed"
)

// Level is the level of internal telemetry (metrics, logs, traces about the component itself)
// that every component should generate.
type Level int32

func (l Level) String() string {
	switch l {
	case LevelNone:
		return levelNoneStr
	case LevelBasic:
		return levelBasicStr
	case LevelNormal:
		return levelNormalStr
	case LevelDetailed:
		return levelDetailedStr
	}
	return ""
}

// MarshalText marshals Level to text.
func (l Level) MarshalText() (text []byte, err error) {
	return []byte(l.String()), nil
}

// UnmarshalText unmarshalls text to a Level.
func (l *Level) UnmarshalText(text []byte) error {
	if l == nil {
		return errors.New("cannot unmarshal to a nil *Level")
	}

	str := strings.ToLower(string(text))
	switch str {
	case strings.ToLower(levelNoneStr):
		*l = LevelNone
		return nil
	case strings.ToLower(levelBasicStr):
		*l = LevelBasic
		return nil
	case strings.ToLower(levelNormalStr):
		*l = LevelNormal
		return nil
	case strings.ToLower(levelDetailedStr):
		*l = LevelDetailed
		return nil
	}
	return fmt.Errorf("unknown metrics level %q", str)
}
