package main

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/prometheus/prometheus/pkg/labels"
)

// Outputs is an enum with all possible output modes
var Outputs = map[string]LogOutput{
	"default": &DefaultOutput{},
	"jsonl":   &JSONLOutput{},
	"raw":     &RawOutput{},
}

// LogOutput is the interface any output mode must implement
type LogOutput interface {
	Print(ts time.Time, lbls *labels.Labels, line string)
}

// DefaultOutput provides logs and metadata in human readable format
type DefaultOutput struct {
	MaxLabelsLen int
	CommonLabels labels.Labels
}

// Print a log entry in a human readable format
func (f DefaultOutput) Print(ts time.Time, lbls *labels.Labels, line string) {
	ls := subtract(*lbls, f.CommonLabels)
	if len(*ignoreLabelsKey) > 0 {
		ls = ls.MatchLabels(false, *ignoreLabelsKey...)
	}

	labels := ""
	if !*noLabels {
		labels = padLabel(ls, f.MaxLabelsLen)
	}
	fmt.Println(
		color.BlueString(ts.Format(time.RFC3339)),
		color.RedString(labels),
		strings.TrimSpace(line),
	)
}

// JSONLOutput prints logs and metadata as JSON Lines, suitable for scripts
type JSONLOutput struct{}

// Print a log entry as json line
func (f JSONLOutput) Print(ts time.Time, lbls *labels.Labels, line string) {
	entry := map[string]interface{}{
		"timestamp": ts,
		"labels":    lbls,
		"line":      line,
	}
	out, err := json.Marshal(entry)
	if err != nil {
		log.Fatalf("error marshalling entry: %s", err)
	}
	fmt.Println(string(out))
}

// RawOutput prints logs in their original form, without any metadata
type RawOutput struct{}

// Print a log entry as is
func (f RawOutput) Print(ts time.Time, lbls *labels.Labels, line string) {
	fmt.Println(line)
}
