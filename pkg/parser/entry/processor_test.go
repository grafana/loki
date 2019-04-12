package entry

import (
	"testing"

	"gopkg.in/yaml.v2"
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
	var config Config
	err := yaml.Unmarshal([]byte(testYaml), &config)
	if err != nil {
		panic(err)
	}
}
