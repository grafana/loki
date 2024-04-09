package base

import (
	"fmt"
	"testing"
	"time"

	config_util "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/prometheus/prometheus/config"
	"github.com/prometheus/prometheus/discovery"
	"github.com/prometheus/prometheus/discovery/dns"
	"github.com/prometheus/prometheus/model/labels"
	"github.com/prometheus/prometheus/model/relabel"
	"github.com/stretchr/testify/require"

	ruler_config "github.com/grafana/loki/v3/pkg/ruler/config"
	"github.com/grafana/loki/v3/pkg/util"
)

func TestBuildNotifierConfig(t *testing.T) {
	tests := []struct {
		name string
		cfg  *Config
		ncfg *config.Config
		err  error
	}{
		{
			name: "with no valid hosts, returns an empty config",
			cfg:  &Config{},
			ncfg: &config.Config{},
		},
		{
			name: "with a single URL and no service discovery",
			cfg: &Config{
				AlertManagerConfig: ruler_config.AlertManagerConfig{
					AlertmanagerURL: "http://alertmanager.default.svc.cluster.local/alertmanager",
				},
			},
			ncfg: &config.Config{
				AlertingConfig: config.AlertingConfig{
					AlertmanagerConfigs: []*config.AlertmanagerConfig{
						{
							APIVersion: "v1",
							Scheme:     "http",
							PathPrefix: "/alertmanager",
							ServiceDiscoveryConfigs: discovery.Configs{
								discovery.StaticConfig{
									{
										Targets: []model.LabelSet{{"__address__": "alertmanager.default.svc.cluster.local"}},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with a single URL and service discovery",
			cfg: &Config{
				AlertManagerConfig: ruler_config.AlertManagerConfig{
					AlertmanagerURL:             "http://_http._tcp.alertmanager.default.svc.cluster.local/alertmanager",
					AlertmanagerDiscovery:       true,
					AlertmanagerRefreshInterval: time.Duration(60),
				},
			},
			ncfg: &config.Config{
				AlertingConfig: config.AlertingConfig{
					AlertmanagerConfigs: []*config.AlertmanagerConfig{
						{
							APIVersion: "v1",
							Scheme:     "http",
							PathPrefix: "/alertmanager",
							ServiceDiscoveryConfigs: discovery.Configs{
								&dns.SDConfig{
									Names:           []string{"_http._tcp.alertmanager.default.svc.cluster.local"},
									RefreshInterval: 60,
									Type:            "SRV",
									Port:            0,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with service discovery and an invalid URL",
			cfg: &Config{
				AlertManagerConfig: ruler_config.AlertManagerConfig{
					AlertmanagerURL:       "http://_http.default.svc.cluster.local/alertmanager",
					AlertmanagerDiscovery: true,
				},
			},
			err: fmt.Errorf("when alertmanager-discovery is on, host name must be of the form _portname._tcp.service.fqdn (is \"alertmanager.default.svc.cluster.local\")"),
		},
		{
			name: "with multiple URLs and no service discovery",
			cfg: &Config{
				AlertManagerConfig: ruler_config.AlertManagerConfig{
					AlertmanagerURL: "http://alertmanager-0.default.svc.cluster.local/alertmanager,http://alertmanager-1.default.svc.cluster.local/alertmanager",
				},
			},
			ncfg: &config.Config{
				AlertingConfig: config.AlertingConfig{
					AlertmanagerConfigs: []*config.AlertmanagerConfig{
						{
							APIVersion: "v1",
							Scheme:     "http",
							PathPrefix: "/alertmanager",
							ServiceDiscoveryConfigs: discovery.Configs{
								discovery.StaticConfig{{
									Targets: []model.LabelSet{{"__address__": "alertmanager-0.default.svc.cluster.local"}},
								},
								},
							},
						},
						{
							APIVersion: "v1",
							Scheme:     "http",
							PathPrefix: "/alertmanager",
							ServiceDiscoveryConfigs: discovery.Configs{
								discovery.StaticConfig{{
									Targets: []model.LabelSet{{"__address__": "alertmanager-1.default.svc.cluster.local"}},
								},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with multiple URLs and service discovery",
			cfg: &Config{
				AlertManagerConfig: ruler_config.AlertManagerConfig{
					AlertmanagerURL:             "http://_http._tcp.alertmanager-0.default.svc.cluster.local/alertmanager,http://_http._tcp.alertmanager-1.default.svc.cluster.local/alertmanager",
					AlertmanagerDiscovery:       true,
					AlertmanagerRefreshInterval: time.Duration(60),
				},
			},
			ncfg: &config.Config{
				AlertingConfig: config.AlertingConfig{
					AlertmanagerConfigs: []*config.AlertmanagerConfig{
						{
							APIVersion: "v1",
							Scheme:     "http",
							PathPrefix: "/alertmanager",
							ServiceDiscoveryConfigs: discovery.Configs{
								&dns.SDConfig{
									Names:           []string{"_http._tcp.alertmanager-0.default.svc.cluster.local"},
									RefreshInterval: 60,
									Type:            "SRV",
									Port:            0,
								},
							},
						},
						{
							APIVersion: "v1",
							Scheme:     "http",
							PathPrefix: "/alertmanager",
							ServiceDiscoveryConfigs: discovery.Configs{
								&dns.SDConfig{
									Names:           []string{"_http._tcp.alertmanager-1.default.svc.cluster.local"},
									RefreshInterval: 60,
									Type:            "SRV",
									Port:            0,
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with Basic Authentication URL",
			cfg: &Config{
				AlertManagerConfig: ruler_config.AlertManagerConfig{
					AlertmanagerURL: "http://marco:hunter2@alertmanager-0.default.svc.cluster.local/alertmanager",
				},
			},
			ncfg: &config.Config{
				AlertingConfig: config.AlertingConfig{
					AlertmanagerConfigs: []*config.AlertmanagerConfig{
						{
							HTTPClientConfig: config_util.HTTPClientConfig{
								BasicAuth: &config_util.BasicAuth{Username: "marco", Password: "hunter2"},
							},
							APIVersion: "v1",
							Scheme:     "http",
							PathPrefix: "/alertmanager",
							ServiceDiscoveryConfigs: discovery.Configs{
								discovery.StaticConfig{
									{
										Targets: []model.LabelSet{{"__address__": "alertmanager-0.default.svc.cluster.local"}},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with Basic Authentication URL and Explicit",
			cfg: &Config{
				AlertManagerConfig: ruler_config.AlertManagerConfig{
					AlertmanagerURL: "http://marco:hunter2@alertmanager-0.default.svc.cluster.local/alertmanager",
					Notifier: ruler_config.NotifierConfig{
						BasicAuth: util.BasicAuth{
							Username: "jacob",
							Password: "test",
						},
					},
				},
			},
			ncfg: &config.Config{
				AlertingConfig: config.AlertingConfig{
					AlertmanagerConfigs: []*config.AlertmanagerConfig{
						{
							HTTPClientConfig: config_util.HTTPClientConfig{
								BasicAuth: &config_util.BasicAuth{Username: "jacob", Password: "test"},
							},
							APIVersion: "v1",
							Scheme:     "http",
							PathPrefix: "/alertmanager",
							ServiceDiscoveryConfigs: discovery.Configs{
								discovery.StaticConfig{
									{
										Targets: []model.LabelSet{{"__address__": "alertmanager-0.default.svc.cluster.local"}},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with Header Authorization",
			cfg: &Config{
				AlertManagerConfig: ruler_config.AlertManagerConfig{
					AlertmanagerURL: "http://alertmanager-0.default.svc.cluster.local/alertmanager",
					Notifier: ruler_config.NotifierConfig{
						HeaderAuth: util.HeaderAuth{
							Type:        "Bearer",
							Credentials: "jacob",
						},
					},
				},
			},
			ncfg: &config.Config{
				AlertingConfig: config.AlertingConfig{
					AlertmanagerConfigs: []*config.AlertmanagerConfig{
						{
							HTTPClientConfig: config_util.HTTPClientConfig{
								Authorization: &config_util.Authorization{
									Type:        "Bearer",
									Credentials: config_util.Secret("jacob"),
								},
							},
							APIVersion: "v1",
							Scheme:     "http",
							PathPrefix: "/alertmanager",
							ServiceDiscoveryConfigs: discovery.Configs{
								discovery.StaticConfig{
									{
										Targets: []model.LabelSet{{"__address__": "alertmanager-0.default.svc.cluster.local"}},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with Header Authorization and credentials file",
			cfg: &Config{
				AlertManagerConfig: ruler_config.AlertManagerConfig{
					AlertmanagerURL: "http://alertmanager-0.default.svc.cluster.local/alertmanager",
					Notifier: ruler_config.NotifierConfig{
						HeaderAuth: util.HeaderAuth{
							Type:            "Bearer",
							CredentialsFile: "/path/to/secret/file",
						},
					},
				},
			},
			ncfg: &config.Config{
				AlertingConfig: config.AlertingConfig{
					AlertmanagerConfigs: []*config.AlertmanagerConfig{
						{
							HTTPClientConfig: config_util.HTTPClientConfig{
								Authorization: &config_util.Authorization{
									Type:            "Bearer",
									CredentialsFile: "/path/to/secret/file",
								},
							},
							APIVersion: "v1",
							Scheme:     "http",
							PathPrefix: "/alertmanager",
							ServiceDiscoveryConfigs: discovery.Configs{
								discovery.StaticConfig{
									{
										Targets: []model.LabelSet{{"__address__": "alertmanager-0.default.svc.cluster.local"}},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "with external labels",
			cfg: &Config{
				AlertManagerConfig: ruler_config.AlertManagerConfig{
					AlertmanagerURL: "http://alertmanager.default.svc.cluster.local/alertmanager",
				},
				ExternalLabels: []labels.Label{
					{Name: "region", Value: "us-east-1"},
				},
			},
			ncfg: &config.Config{
				AlertingConfig: config.AlertingConfig{
					AlertmanagerConfigs: []*config.AlertmanagerConfig{
						{
							APIVersion: "v1",
							Scheme:     "http",
							PathPrefix: "/alertmanager",
							ServiceDiscoveryConfigs: discovery.Configs{
								discovery.StaticConfig{
									{
										Targets: []model.LabelSet{{"__address__": "alertmanager.default.svc.cluster.local"}},
									},
								},
							},
						},
					},
				},
				GlobalConfig: config.GlobalConfig{
					ExternalLabels: []labels.Label{
						{Name: "region", Value: "us-east-1"},
					},
				},
			},
		},
		{
			name: "with alert relabel config",
			cfg: &Config{
				AlertManagerConfig: ruler_config.AlertManagerConfig{
					AlertmanagerURL: "http://alertmanager.default.svc.cluster.local/alertmanager",
					AlertRelabelConfigs: []*relabel.Config{
						{
							SourceLabels: model.LabelNames{"severity"},
							Regex:        relabel.MustNewRegexp("high"),
							TargetLabel:  "priority",
							Replacement:  "p1",
						},
					},
				},
				ExternalLabels: []labels.Label{
					{Name: "region", Value: "us-east-1"},
				},
			},
			ncfg: &config.Config{
				AlertingConfig: config.AlertingConfig{
					AlertmanagerConfigs: []*config.AlertmanagerConfig{
						{
							APIVersion: "v1",
							Scheme:     "http",
							PathPrefix: "/alertmanager",
							ServiceDiscoveryConfigs: discovery.Configs{
								discovery.StaticConfig{
									{
										Targets: []model.LabelSet{{"__address__": "alertmanager.default.svc.cluster.local"}},
									},
								},
							},
						},
					},
					AlertRelabelConfigs: []*relabel.Config{
						{
							SourceLabels: model.LabelNames{"severity"},
							Regex:        relabel.MustNewRegexp("high"),
							TargetLabel:  "priority",
							Replacement:  "p1",
						},
					},
				},
				GlobalConfig: config.GlobalConfig{
					ExternalLabels: []labels.Label{
						{Name: "region", Value: "us-east-1"},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ncfg, err := buildNotifierConfig(&tt.cfg.AlertManagerConfig, tt.cfg.ExternalLabels)
			if tt.err == nil {
				require.NoError(t, err)
				require.Equal(t, tt.ncfg, ncfg)
			} else {
				require.Error(t, tt.err, err)
			}
		})
	}
}

func TestApplyAlertmanagerDefaults(t *testing.T) {
	tests := []struct {
		name     string
		amConfig ruler_config.AlertManagerConfig
		want     ruler_config.AlertManagerConfig
	}{
		{
			name:     "with an empty config, returns the default values",
			amConfig: ruler_config.AlertManagerConfig{},
			want: ruler_config.AlertManagerConfig{
				AlertmanagerRefreshInterval: 1 * time.Minute,
				NotificationQueueCapacity:   10000,
				NotificationTimeout:         10 * time.Second,
			},
		},
		{
			name: "apply default values for the values that are undefined",
			amConfig: ruler_config.AlertManagerConfig{
				AlertmanagerURL:          "url",
				AlertmanangerEnableV2API: true,
				AlertmanagerDiscovery:    true,
			},
			want: ruler_config.AlertManagerConfig{
				AlertmanagerURL:             "url",
				AlertmanangerEnableV2API:    true,
				AlertmanagerDiscovery:       true,
				AlertmanagerRefreshInterval: 1 * time.Minute,
				NotificationQueueCapacity:   10000,
				NotificationTimeout:         10 * time.Second,
			},
		},
		{
			name: "do not apply default values for the values that are defined",
			amConfig: ruler_config.AlertManagerConfig{
				AlertmanagerURL:             "url",
				AlertmanangerEnableV2API:    true,
				AlertmanagerDiscovery:       true,
				AlertmanagerRefreshInterval: 2 * time.Minute,
				NotificationQueueCapacity:   20000,
				NotificationTimeout:         20 * time.Second,
			},
			want: ruler_config.AlertManagerConfig{
				AlertmanagerURL:             "url",
				AlertmanangerEnableV2API:    true,
				AlertmanagerDiscovery:       true,
				AlertmanagerRefreshInterval: 2 * time.Minute,
				NotificationQueueCapacity:   20000,
				NotificationTimeout:         20 * time.Second,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := applyAlertmanagerDefaults(tt.amConfig)
			require.Equal(t, tt.want, result)
		})
	}
}
