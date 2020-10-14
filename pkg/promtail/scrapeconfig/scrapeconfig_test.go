package scrapeconfig

import (
	"testing"

	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/discovery/kubernetes"
	"github.com/prometheus/prometheus/discovery/targetgroup"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

// todo add full example.
var testYaml = `
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

func TestLoadConfig(t *testing.T) {
	var config Config
	err := yaml.Unmarshal([]byte(testYaml), &config)
	require.Nil(t, err)

	expected := Config{
		JobName:        "kubernetes-pods-name",
		PipelineStages: DefaultScrapeConfig.PipelineStages,
		ServiceDiscoveryConfig: ServiceDiscoveryConfig{
			KubernetesSDConfigs: []*kubernetes.SDConfig{
				{
					Role: "pod",
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
