// Copyright The OpenTelemetry Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//       http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package configtelemetry // import "go.opentelemetry.io/collector/config/configtelemetry"

import (
	"encoding"
	"flag"
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

	levelNoneStr     = "none"
	levelBasicStr    = "basic"
	levelNormalStr   = "normal"
	levelDetailedStr = "detailed"
)

const UseOpenTelemetryForInternalMetrics = false

// Flags is a helper function to add telemetry config flags to the service that exposes
// the application flags.
// Deprecated: No-op, kept for backwards compatibility until v0.44.0.
func Flags(*flag.FlagSet) {}

// Level is the level of internal telemetry (metrics, logs, traces about the component itself)
// that every component should generate.
type Level int32

var _ flag.Value = (*Level)(nil)
var _ encoding.TextUnmarshaler = (*Level)(nil)

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
	return "unknown"
}

// Deprecated: will be removed in v0.44.0.
func (l *Level) Set(s string) error {
	lvl, err := parseLevel(s)
	if err != nil {
		return err
	}
	*l = lvl
	return nil
}

// UnmarshalText unmarshals text to a Level.
func (l *Level) UnmarshalText(text []byte) error {
	if l == nil {
		return fmt.Errorf("cannot unmarshal to a nil *Level")
	}
	var err error
	*l, err = parseLevel(string(text))
	return err
}

// parseLevel returns the Level represented by the string. The parsing is case-insensitive
// and it returns error if the string value is unknown.
func parseLevel(str string) (Level, error) {
	str = strings.ToLower(str)

	switch str {
	case levelNoneStr:
		return LevelNone, nil
	case levelBasicStr:
		return LevelBasic, nil
	case levelNormalStr:
		return LevelNormal, nil
	case levelDetailedStr:
		return LevelDetailed, nil
	}
	return LevelNone, fmt.Errorf("unknown metrics level %q", str)
}
