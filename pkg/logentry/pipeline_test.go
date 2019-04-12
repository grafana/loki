package logentry

import (
	"testing"

	"gopkg.in/yaml.v2"

	"github.com/grafana/loki/pkg/logentry/stages"
)

var testYaml = `
parser_stages:
  - regex:
      expr: ./*
      labels:
        - test:
          source: somesource
      
`

func TestNewProcessor(t *testing.T) {
	var config stages.Config
	err := yaml.Unmarshal([]byte(testYaml), &config)
	if err != nil {
		panic(err)
	}
}
