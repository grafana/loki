package logentry

import (
	"testing"

	"github.com/cortexproject/cortex/pkg/util"

	"gopkg.in/yaml.v2"
)

var testYaml = `
pipeline_stages:
- regex:
    expr: "./*"
- json: 
    timestamp:
      source: time
      format: RFC3339
    labels:
      stream:
        source: json_key_name.json_sub_key_name
    output:
      source: log
`

func TestNewPipeline(t *testing.T) {
	var config map[string]interface{}
	err := yaml.Unmarshal([]byte(testYaml), &config)
	if err != nil {
		panic(err)
	}
	p, err := NewPipeline(util.Logger, config["pipeline_stages"].([]interface{}))
	if err != nil {
		panic(err)
	}
	if len(p.stages) != 1 {
		t.Fatal("missing stages")
	}
}
