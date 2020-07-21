package lokipush

import (
	"testing"

	"github.com/weaveworks/common/server"

	"github.com/grafana/loki/pkg/promtail/scrapeconfig"
)

func Test_validateJobName(t *testing.T) {
	tests := []struct {
		name    string
		configs []scrapeconfig.Config
		// Only validated against the first job in the provided scrape configs
		expectedJob string
		wantErr     bool
	}{
		{
			name: "valid with spaces removed",
			configs: []scrapeconfig.Config{
				{
					JobName: "jobby job job",
					PushConfig: &scrapeconfig.PushTargetConfig{
						Server: server.Config{},
					},
				},
			},
			wantErr:     false,
			expectedJob: "jobby_job_job",
		},
		{
			name: "missing job",
			configs: []scrapeconfig.Config{
				{
					PushConfig: &scrapeconfig.PushTargetConfig{
						Server: server.Config{},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "duplicate job",
			configs: []scrapeconfig.Config{
				{
					JobName: "job1",
					PushConfig: &scrapeconfig.PushTargetConfig{
						Server: server.Config{},
					},
				},
				{
					JobName: "job1",
					PushConfig: &scrapeconfig.PushTargetConfig{
						Server: server.Config{},
					},
				},
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateJobName(tt.configs)
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
