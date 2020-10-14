package config

import (
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v2"
)

const testFile = `
clients:
  - external_labels:
        cluster: dev1
    url: https://1:shh@example.com/loki/api/v1/push
  - external_labels:
        cluster: prod1
    url: https://1:shh@example.com/loki/api/v1/push
scrape_configs:
  - job_name: kubernetes-pods-name
    kubernetes_sd_configs:
      - role: pod
  - job_name: system
    static_configs:
    - targets:
      - localhost
      labels:
        job: varlogs
`

func Test_Load(t *testing.T) {

	var dst Config
	err := yaml.Unmarshal([]byte(testFile), &dst)
	require.Nil(t, err)
}
