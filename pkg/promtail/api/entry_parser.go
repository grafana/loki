package api

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/prometheus/common/model"
)

// EntryParser describes how to parse log lines.
type EntryParser int

// Different supported EntryParsers.
const (
	Docker EntryParser = iota
	Raw
	CRI
)

var (
	criPattern = regexp.MustCompile(`^(?s)(?P<time>\S+?) (?P<stream>stdout|stderr) (?P<flags>\S+?) (?P<content>.*)$`)
)

// String returns a string representation of the EntryParser.
func (e EntryParser) String() string {
	switch e {
	case CRI:
		return "cri"
	case Docker:
		return "docker"
	case Raw:
		return "raw"
	default:
		panic(e)
	}
}

// Set implements flag.Value.
func (e *EntryParser) Set(s string) error {
	switch strings.ToLower(s) {
	case "cri":
		*e = CRI
		return nil
	case "docker":
		*e = Docker
		return nil
	case "raw":
		*e = Raw
		return nil
	default:
		return fmt.Errorf("unrecognised EntryParser: %v", s)
	}
}

// UnmarshalYAML implements yaml.Unmarshaler.
func (e *EntryParser) UnmarshalYAML(unmarshal func(interface{}) error) error {
	var s string
	if err := unmarshal(&s); err != nil {
		return err
	}
	return e.Set(s)
}

// Wrap implements EntryMiddleware.
func (e EntryParser) Wrap(next EntryHandler) EntryHandler {
	switch e {
	case CRI:
		return EntryHandlerFunc(func(labels model.LabelSet, _ time.Time, line string) error {
			parts := criPattern.FindStringSubmatch(line)
			if parts == nil || len(parts) < 5 {
				return fmt.Errorf("Line did not match the CRI log format: '%s'", line)
			}

			timestamp, err := time.Parse(time.RFC3339Nano, parts[1])
			if err != nil {
				return fmt.Errorf("CRI timestamp '%s' does not match RFC3339Nano", parts[1])
			}

			labels = labels.Merge(model.LabelSet{"stream": model.LabelValue(parts[2])})
			return next.Handle(labels, timestamp, parts[4])
		})
	case Docker:
		return EntryHandlerFunc(func(labels model.LabelSet, _ time.Time, line string) error {
			// Docker-style json object per line.
			var entry struct {
				Log    string
				Stream string
				Time   time.Time
			}
			if err := json.Unmarshal([]byte(line), &entry); err != nil {
				return err
			}
			labels = labels.Merge(model.LabelSet{"stream": model.LabelValue(entry.Stream)})
			return next.Handle(labels, entry.Time, entry.Log)
		})
	case Raw:
		return next
	default:
		panic(fmt.Sprintf("unrecognised EntryParser: %s", e))
	}
}
