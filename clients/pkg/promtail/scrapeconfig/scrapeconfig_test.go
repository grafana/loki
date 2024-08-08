package scrapeconfig

import (
	"testing"

	promConfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/kubernetes"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

// todo add full example.
var smallYaml = `
job_name: kubernetes-pods-name
static_configs:
- targets:
    - localhost
  labels:
    job: varlogs
    __path__: /var/log/*log
kubernetes_sd_configs:
- role: pod
`

// todo add full example.
var testYaml = `
pipeline_stages:
  - regex:
      expr: "./*"
  - json:
      expressions:
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

var noPipelineStagesYaml = `
job_name: kubernetes-pods-name
static_configs:
- targets:
    - localhost
  labels:
    job: varlogs
    __path__: /var/log/*log
kubernetes_sd_configs:
- role: pod
`

func TestLoadSmallConfig(t *testing.T) {
	var config Config
	err := yaml.Unmarshal([]byte(smallYaml), &config)
	require.Nil(t, err)

	expected := Config{
		JobName:        "kubernetes-pods-name",
		PipelineStages: DefaultScrapeConfig.PipelineStages,
		ServiceDiscoveryConfig: ServiceDiscoveryConfig{
			KubernetesSDConfigs: []*kubernetes.SDConfig{
				{
					Role:             "pod",
					HTTPClientConfig: promConfig.DefaultHTTPClientConfig,
				},
			},
			StaticConfigs: []*targetgroup.Group{
				{
					Targets: []model.LabelSet{{"__address__": "localhost"}},
					Labels: map[model.LabelName]model.LabelValue{
						"job":      "varlogs",
						"__path__": "/var/log/*log",
					},
					Source: "",
				},
			},
		},
	}
	require.Equal(t, expected, config)
}

// bugfix: https://github.com/grafana/loki/issues/3403
func TestEmptyPipelineStagesConfig(t *testing.T) {
	var config Config
	err := yaml.Unmarshal([]byte(noPipelineStagesYaml), &config)
	require.Nil(t, err)

	require.Zero(t, len(config.PipelineStages))
}

func TestLoadConfig(t *testing.T) {
	var config Config
	err := yaml.Unmarshal([]byte(testYaml), &config)
	if err != nil {
		panic(err)
	}

	require.NotZero(t, len(config.PipelineStages))
}

func Test_validateJobName(t *testing.T) {
	tests := []struct {
		name    string
		configs []Config
		// Only validated against the first job in the provided scrape configs
		expectedJob string
		wantErr     bool
	}{
		{
			name: "valid with spaces removed",
			configs: []Config{
				{
					JobName: "jobby job job",
				},
			},
			wantErr:     false,
			expectedJob: "jobby_job_job",
		},
		{
			name: "missing job",
			configs: []Config{
				{},
			},
			wantErr: true,
		},
		{
			name: "duplicate job",
			configs: []Config{
				{
					JobName: "job1",
				},
				{
					JobName: "job1",
				},
			},
			wantErr: true,
		},
		{
			name: "validate with special characters",
			configs: []Config{
				{
					JobName: "job$1-2!3@4*job",
				},
			},
			wantErr:     false,
			expectedJob: "job_1_2_3_4_job",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateJobName(tt.configs)
			if (err != nil) != tt.wantErr {
				t.Errorf("validateJobName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr {
				if tt.configs[0].JobName != tt.expectedJob {
					t.Errorf("Expected to find a job with name %v but did not find it", tt.expectedJob)
					return
				}
			}
		})
	}
}
