package api

import (
	"fmt"
	"os"
	"time"

	"github.com/prometheus/common/model"
)

// EnvironmentParser describes how to parse log lines.
type EnvironmentParser struct {
	Configs     []EnvironmentConfig
	Environment envMap
}

type envMap map[string]string

// DefaultEnvironmentConfig is the default EnvironmentConfig.
var DefaultEnvironmentConfig = EnvironmentConfig{}

// String returns a string representation of the EnvironmentParser.
func (e EnvironmentParser) String() string {
	return "environmentparser"
}

// Set implements flag.Value.
func (e *EnvironmentParser) Set(configs *[]EnvironmentConfig) error {
	e.Configs = *configs
	return nil
}

// UnmarshalYAML implements the yaml.Unmarshaler interface.
func (e *EnvironmentParser) UnmarshalYAML(unmarshal func(interface{}) error) error {

	configs := &[]EnvironmentConfig{}

	*e = EnvironmentParser{*configs, make(envMap)}

	if err := unmarshal(configs); err != nil {
		return err
	}

	for _, c := range *configs {
		if len(c.LabelName) == 0 {
			return fmt.Errorf("label_name is empty")
		}
		if len(c.EnvironmentVariable) == 0 {
			return fmt.Errorf("environment_variable is empty")
		}
		e.Environment[c.LabelName] = os.Getenv(c.EnvironmentVariable)
	}

	return e.Set(configs)
}

// Wrap implements EntryMiddleware.
func (e EnvironmentParser) Wrap(next EntryHandler) EntryHandler {

	return EntryHandlerFunc(func(labels model.LabelSet, t time.Time, line string) error {
		for labelName, value := range e.Environment {
			labels = labels.Merge(model.LabelSet{model.LabelName(labelName): model.LabelValue(value)})
		}

		return next.Handle(labels, t, line)
	})

}
