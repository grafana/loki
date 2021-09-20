package cluster

import (
	"fmt"
	"strings"
	"testing"

	"github.com/grafana/agent/pkg/metrics/instance"
	"github.com/grafana/agent/pkg/util"
	"github.com/stretchr/testify/require"
)

func Test_validateNoFiles(t *testing.T) {
	tt := []struct {
		name   string
		input  string
		expect error
	}{
		{
			name: "valid config",
			input: util.Untab(`
			scrape_configs:
			- job_name: innocent_scrape
				static_configs:
					- targets: ['127.0.0.1:12345']
			remote_write:
			- url: http://localhost:9009/api/prom/push
			`),
			expect: nil,
		},
		{
			name: "all SDs",
			input: util.Untab(`
      scrape_configs:
			- job_name: basic_sds
				static_configs:
				- targets: ['localhost']
				azure_sd_configs:
				- subscription_id: fake
					tenant_id: fake
					client_id: fake
					client_secret: fake
				consul_sd_configs:
				- {}
				dns_sd_configs:
				- names: ['fake']
				ec2_sd_configs:
				- region: fake
				eureka_sd_configs:
				- server: http://localhost:80/eureka
				file_sd_configs:
				- files: ['fake.json']
				digitalocean_sd_configs:
				- {}
				dockerswarm_sd_configs:
				- host: localhost
					role: nodes
				gce_sd_configs:
				- project: fake
					zone: fake
				hetzner_sd_configs:
				- role: hcloud
				kubernetes_sd_configs:
				- role: pod
				marathon_sd_configs:
				- servers: ['localhost']
				nerve_sd_configs:
				- servers: ['localhost']
					paths: ['/']
				openstack_sd_configs:
				- role: instance
					region: fake
				scaleway_sd_configs:
				- role: instance
					project_id: ffffffff-ffff-ffff-ffff-ffffffffffff
					secret_key: ffffffff-ffff-ffff-ffff-ffffffffffff
					access_key: SCWXXXXXXXXXXXXXXXXX
				serverset_sd_configs:
				- servers: ['localhost']
					paths: ['/']
				triton_sd_configs:
				- account: fake
					dns_suffix: fake
					endpoint: fake
			`),
			expect: nil,
		},
		{
			name: "invalid http client config",
			input: util.Untab(`
			scrape_configs:
			- job_name: malicious_scrape
				static_configs:
					- targets: ['badsite.com']
				basic_auth:
					username: file_leak
					password_file: /etc/password
			remote_write:
			- url: http://localhost:9009/api/prom/push
			`),
			expect: fmt.Errorf("failed to validate scrape_config at index 0: password_file must be empty unless dangerous_allow_reading_files is set"),
		},
	}

	for _, tc := range tt {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := instance.UnmarshalConfig(strings.NewReader(tc.input))
			require.NoError(t, err)

			actual := validateNofiles(cfg)
			if tc.expect == nil {
				require.NoError(t, actual)
			} else {
				require.EqualError(t, actual, tc.expect.Error())
			}
		})
	}
}
