package api

import (
	"fmt"
	"regexp"
	"time"

	"github.com/prometheus/common/model"
)

// CaptureParser describes how to parse log lines.
type CaptureParser struct {
	Configs []CaptureConfig
}

// DefaultCaptureConfig is the default ScrapeConfig.
var DefaultCaptureConfig = CaptureConfig{}

// String returns a string representation of the CaptureParser.
func (e CaptureParser) String() string {
	fmt.Println("capture String()")
	return "captureparser"
}

// Set implements flag.Value.
func (cp *CaptureParser) Set(configs *[]CaptureConfig) error {
	*cp = CaptureParser{*configs}
	return nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (cp *CaptureParser) UnmarshalYAML(unmarshal func(interface{}) error) error {

	configs := &[]CaptureConfig{}

	// TODO: line in Entry does (*plain), unclear why.
	if err := unmarshal(configs); err != nil {
		return err
	}

	for _, c := range *configs {
		if len(c.Regex) == 0 {
			return fmt.Errorf("regex is empty")
		}
		if len(c.Template) == 0 {
			return fmt.Errorf("template is empty")
		}
		if len(c.LabelName) == 0 {
			return fmt.Errorf("label_name is empty")
		}
	}

	return cp.Set(configs)
}

// Wrap implements EntryMiddleware.
func (e CaptureParser) Wrap(next EntryHandler) EntryHandler {
	fmt.Println("!!!Capture Wrap called")

	re, _ := regexp.Compile(".*level=([a-zA-Z]+).*")
	return EntryHandlerFunc(func(labels model.LabelSet, t time.Time, line string) error {
		matched := re.FindStringSubmatch(line)
		fmt.Println(matched)
		// Get label names as array from YAML
		// for each matched:
		// labels = labels.Merge(labels[index], matched[index])
		// labels = labels.Merge(model.LabelSet{"stream": model.LabelValue(entry.Stream)})
		return next.Handle(labels, t, line)
	})

}
