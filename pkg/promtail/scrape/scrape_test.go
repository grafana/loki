package scrape

import (
	"testing"

	"gopkg.in/yaml.v2"
)

// todo add full example.
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
job_name: kubernetes-pods-name
kubernetes_sd_configs:
- role: pod
relabel_configs:
- source_labels:
  - __meta_kubernetes_pod_label_name
  target_label: __service__
- source_labels:
  - __meta_kubernetes_pod_node_name
  target_label: __host__
- action: drop
  regex: ^$
  source_labels:
  - __service__
- action: replace
  replacement: $1
  separator: /
  source_labels:
  - __meta_kubernetes_namespace
  - __service__
  target_label: job
- action: replace
  source_labels:
  - __meta_kubernetes_namespace
  target_label: namespace
- action: replace
  source_labels:
  - __meta_kubernetes_pod_name
  target_label: instance
- action: replace
  source_labels:
  - __meta_kubernetes_pod_container_name
  target_label: container_name
- action: labelmap
  regex: __meta_kubernetes_pod_label_(.+)
- replacement: /var/log/pods/$1/*.log
  separator: /
  source_labels:
  - __meta_kubernetes_pod_uid
  - __meta_kubernetes_pod_container_name
  target_label: __path__
`

func TestLoadConfig(t *testing.T) {
	var config Config
	err := yaml.Unmarshal([]byte(testYaml), &config)
	if err != nil {
		panic(err)
	}
}
