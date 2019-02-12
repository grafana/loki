package api

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
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

	return EntryHandlerFunc(func(labels model.LabelSet, t time.Time, line string) error {
		for _, config := range e.Configs {
			// TODO: precompile
			re, _ := regexp.Compile(config.Regex)
			matched := re.FindStringSubmatch(line)

			labelValue := config.Template
			for idx, value := range matched[1:] {
				replacement := strings.Replace("${n}", "n", strconv.Itoa(idx+1), 1)
				labelValue = strings.Replace(labelValue, replacement, value, 1)

			}

			labels = labels.Merge(model.LabelSet{model.LabelName(config.LabelName): model.LabelValue(labelValue)})
		}

		return next.Handle(labels, t, line)
	})

}
