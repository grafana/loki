package manifests

import (
	"encoding/json"
	"math/rand"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/internal/config"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
)

func TestConfigMap_ReturnsSHA1OfBinaryContents(t *testing.T) {
	opts := randomConfigOptions()

	_, sha1C, err := LokiConfigMap(opts)
	require.NoError(t, err)
	require.NotEmpty(t, sha1C)
}

func TestConfigOptions_UserOptionsTakePrecedence(t *testing.T) {
	// regardless of what is provided by the default sizing parameters we should always prefer
	// the user-defined values. This creates an all-inclusive Options and then checks
	// that every value is present in the result
	opts := randomConfigOptions()
	res := ConfigOptions(opts)

	expected, err := json.Marshal(opts.Stack)
	require.NoError(t, err)

	actual, err := json.Marshal(res.Stack)
	require.NoError(t, err)

	assert.JSONEq(t, string(expected), string(actual))
}

func testTimeoutConfig() TimeoutConfig {
	return TimeoutConfig{
		Loki: config.HTTPTimeoutConfig{
			IdleTimeout:  1 * time.Second,
			ReadTimeout:  1 * time.Minute,
			WriteTimeout: 10 * time.Minute,
		},
	}
}

func randomConfigOptions() Options {
	return Options{
		Name:      uuid.New().String(),
		Namespace: uuid.New().String(),
		Image:     uuid.New().String(),
		Timeouts:  testTimeoutConfig(),
		Stack: lokiv1.LokiStackSpec{
			Size:             lokiv1.SizeOneXExtraSmall,
			Storage:          lokiv1.ObjectStorageSpec{},
			StorageClassName: uuid.New().String(),
			Replication: &lokiv1.ReplicationSpec{
				Factor: rand.Int31(),
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					IngestionLimits: &lokiv1.IngestionLimitSpec{
						IngestionRate:             rand.Int31(),
						IngestionBurstSize:        rand.Int31(),
						MaxLabelNameLength:        rand.Int31(),
						MaxLabelValueLength:       rand.Int31(),
						MaxLabelNamesPerSeries:    rand.Int31(),
						MaxGlobalStreamsPerTenant: rand.Int31(),
						MaxLineSize:               rand.Int31(),
						PerStreamRateLimit:        rand.Int31(),
						PerStreamRateLimitBurst:   rand.Int31(),
					},
					QueryLimits: &lokiv1.QueryLimitSpec{
						MaxEntriesLimitPerQuery: rand.Int31(),
						MaxChunksPerQuery:       rand.Int31(),
						MaxQuerySeries:          rand.Int31(),
					},
				},
				Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
					uuid.New().String(): {
						IngestionLimits: &lokiv1.IngestionLimitSpec{
							IngestionRate:             rand.Int31(),
							IngestionBurstSize:        rand.Int31(),
							MaxLabelNameLength:        rand.Int31(),
							MaxLabelValueLength:       rand.Int31(),
							MaxLabelNamesPerSeries:    rand.Int31(),
							MaxGlobalStreamsPerTenant: rand.Int31(),
							MaxLineSize:               rand.Int31(),
							PerStreamRateLimit:        rand.Int31(),
							PerStreamRateLimitBurst:   rand.Int31(),
						},
						QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
							QueryLimitSpec: lokiv1.QueryLimitSpec{
								MaxEntriesLimitPerQuery: rand.Int31(),
								MaxChunksPerQuery:       rand.Int31(),
								MaxQuerySeries:          rand.Int31(),
							},
						},
					},
				},
			},
			Template: &lokiv1.LokiTemplateSpec{
				Compactor: &lokiv1.LokiComponentSpec{
					Replicas: 1,
					NodeSelector: map[string]string{
						uuid.New().String(): uuid.New().String(),
					},
					Tolerations: []corev1.Toleration{
						{
							Key:               uuid.New().String(),
							Operator:          corev1.TolerationOpEqual,
							Value:             uuid.New().String(),
							Effect:            corev1.TaintEffectNoExecute,
							TolerationSeconds: ptr.To[int64](rand.Int63()),
						},
					},
				},
				Distributor: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
					NodeSelector: map[string]string{
						uuid.New().String(): uuid.New().String(),
					},
					Tolerations: []corev1.Toleration{
						{
							Key:               uuid.New().String(),
							Operator:          corev1.TolerationOpEqual,
							Value:             uuid.New().String(),
							Effect:            corev1.TaintEffectNoExecute,
							TolerationSeconds: ptr.To[int64](rand.Int63()),
						},
					},
				},
				Ingester: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
					NodeSelector: map[string]string{
						uuid.New().String(): uuid.New().String(),
					},
					Tolerations: []corev1.Toleration{
						{
							Key:               uuid.New().String(),
							Operator:          corev1.TolerationOpEqual,
							Value:             uuid.New().String(),
							Effect:            corev1.TaintEffectNoExecute,
							TolerationSeconds: ptr.To[int64](rand.Int63()),
						},
					},
				},
				Querier: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
					NodeSelector: map[string]string{
						uuid.New().String(): uuid.New().String(),
					},
					Tolerations: []corev1.Toleration{
						{
							Key:               uuid.New().String(),
							Operator:          corev1.TolerationOpEqual,
							Value:             uuid.New().String(),
							Effect:            corev1.TaintEffectNoExecute,
							TolerationSeconds: ptr.To[int64](rand.Int63()),
						},
					},
				},
				QueryFrontend: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
					NodeSelector: map[string]string{
						uuid.New().String(): uuid.New().String(),
					},
					Tolerations: []corev1.Toleration{
						{
							Key:               uuid.New().String(),
							Operator:          corev1.TolerationOpEqual,
							Value:             uuid.New().String(),
							Effect:            corev1.TaintEffectNoExecute,
							TolerationSeconds: ptr.To[int64](rand.Int63()),
						},
					},
				},
				IndexGateway: &lokiv1.LokiComponentSpec{
					Replicas: rand.Int31(),
					NodeSelector: map[string]string{
						uuid.New().String(): uuid.New().String(),
					},
					Tolerations: []corev1.Toleration{
						{
							Key:               uuid.New().String(),
							Operator:          corev1.TolerationOpEqual,
							Value:             uuid.New().String(),
							Effect:            corev1.TaintEffectNoExecute,
							TolerationSeconds: ptr.To[int64](rand.Int63()),
						},
					},
				},
			},
		},
	}
}

func TestConfigOptions_GossipRingConfig(t *testing.T) {
	tt := []struct {
		desc        string
		spec        lokiv1.LokiStackSpec
		wantOptions config.GossipRing
	}{
		{
			desc: "defaults",
			spec: lokiv1.LokiStackSpec{},
			wantOptions: config.GossipRing{
				InstancePort:         9095,
				BindPort:             7946,
				MembersDiscoveryAddr: "my-stack-gossip-ring.my-ns.svc.cluster.local",
			},
		},
		{
			desc: "defaults with empty config",
			spec: lokiv1.LokiStackSpec{
				HashRing: &lokiv1.HashRingSpec{
					Type: lokiv1.HashRingMemberList,
				},
			},
			wantOptions: config.GossipRing{
				InstancePort:         9095,
				BindPort:             7946,
				MembersDiscoveryAddr: "my-stack-gossip-ring.my-ns.svc.cluster.local",
			},
		},
		{
			desc: "user selected any instance addr",
			spec: lokiv1.LokiStackSpec{
				HashRing: &lokiv1.HashRingSpec{
					Type: lokiv1.HashRingMemberList,
					MemberList: &lokiv1.MemberListSpec{
						InstanceAddrType: lokiv1.InstanceAddrDefault,
					},
				},
			},
			wantOptions: config.GossipRing{
				InstancePort:         9095,
				BindPort:             7946,
				MembersDiscoveryAddr: "my-stack-gossip-ring.my-ns.svc.cluster.local",
			},
		},
		{
			desc: "user selected podIP instance addr",
			spec: lokiv1.LokiStackSpec{
				HashRing: &lokiv1.HashRingSpec{
					Type: lokiv1.HashRingMemberList,
					MemberList: &lokiv1.MemberListSpec{
						InstanceAddrType: lokiv1.InstanceAddrPodIP,
					},
				},
			},
			wantOptions: config.GossipRing{
				InstanceAddr:         "${HASH_RING_INSTANCE_ADDR}",
				InstancePort:         9095,
				BindPort:             7946,
				MembersDiscoveryAddr: "my-stack-gossip-ring.my-ns.svc.cluster.local",
			},
		},
		{
			desc: "user selected Topology zone",
			spec: lokiv1.LokiStackSpec{
				Replication: &lokiv1.ReplicationSpec{
					Zones: []lokiv1.ZoneSpec{
						{
							TopologyKey: "testzone",
						},
					},
				},
			},
			wantOptions: config.GossipRing{
				EnableInstanceAvailabilityZone: true,
				InstancePort:                   9095,
				BindPort:                       7946,
				MembersDiscoveryAddr:           "my-stack-gossip-ring.my-ns.svc.cluster.local",
			},
		},
		{
			desc: "IPv6 enabled with default instance address type",
			spec: lokiv1.LokiStackSpec{
				HashRing: &lokiv1.HashRingSpec{
					Type: lokiv1.HashRingMemberList,
					MemberList: &lokiv1.MemberListSpec{
						EnableIPv6: true,
					},
				},
			},
			wantOptions: config.GossipRing{
				EnableIPv6:           true,
				InstanceAddr:         "${HASH_RING_INSTANCE_ADDR}",
				InstancePort:         9095,
				BindPort:             7946,
				MembersDiscoveryAddr: "my-stack-gossip-ring.my-ns.svc.cluster.local",
			},
		},
		{
			desc: "IPv6 enabled with podIP instance address type",
			spec: lokiv1.LokiStackSpec{
				HashRing: &lokiv1.HashRingSpec{
					Type: lokiv1.HashRingMemberList,
					MemberList: &lokiv1.MemberListSpec{
						EnableIPv6:       true,
						InstanceAddrType: lokiv1.InstanceAddrPodIP,
					},
				},
			},
			wantOptions: config.GossipRing{
				EnableIPv6:           true,
				InstanceAddr:         "${HASH_RING_INSTANCE_ADDR}",
				InstancePort:         9095,
				BindPort:             7946,
				MembersDiscoveryAddr: "my-stack-gossip-ring.my-ns.svc.cluster.local",
			},
		},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			inOpt := Options{
				Name:      "my-stack",
				Namespace: "my-ns",
				Stack:     tc.spec,
				Timeouts:  testTimeoutConfig(),
			}
			options := ConfigOptions(inOpt)
			require.Equal(t, tc.wantOptions, options.GossipRing)
		})
	}
}

func TestConfigOptions_RetentionConfig(t *testing.T) {
	tt := []struct {
		desc        string
		spec        lokiv1.LokiStackSpec
		wantOptions config.RetentionOptions
	}{
		{
			desc: "no retention",
			spec: lokiv1.LokiStackSpec{},
			wantOptions: config.RetentionOptions{
				Enabled: false,
			},
		},
		{
			desc: "global retention, extra small",
			spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 14,
						},
					},
				},
			},
			wantOptions: config.RetentionOptions{
				Enabled:           true,
				DeleteWorkerCount: 10,
			},
		},
		{
			desc: "global and tenant retention, extra small",
			spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 14,
						},
					},
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"development": {
							Retention: &lokiv1.RetentionLimitSpec{
								Days: 3,
							},
						},
					},
				},
			},
			wantOptions: config.RetentionOptions{
				Enabled:           true,
				DeleteWorkerCount: 10,
			},
		},
		{
			desc: "tenant retention, extra small",
			spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXExtraSmall,
				Limits: &lokiv1.LimitsSpec{
					Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
						"development": {
							Retention: &lokiv1.RetentionLimitSpec{
								Days: 3,
							},
						},
					},
				},
			},
			wantOptions: config.RetentionOptions{
				Enabled:           true,
				DeleteWorkerCount: 10,
			},
		},
		{
			desc: "global retention, medium",
			spec: lokiv1.LokiStackSpec{
				Size: lokiv1.SizeOneXMedium,
				Limits: &lokiv1.LimitsSpec{
					Global: &lokiv1.LimitsTemplateSpec{
						Retention: &lokiv1.RetentionLimitSpec{
							Days: 14,
						},
					},
				},
			},
			wantOptions: config.RetentionOptions{
				Enabled:           true,
				DeleteWorkerCount: 150,
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			inOpt := Options{
				Stack:    tc.spec,
				Timeouts: testTimeoutConfig(),
			}
			options := ConfigOptions(inOpt)
			require.Equal(t, tc.wantOptions, options.Retention)
		})
	}
}

func TestConfigOptions_RulerAlertManager(t *testing.T) {
	tt := []struct {
		desc        string
		opts        Options
		wantOptions *config.AlertManagerConfig
	}{
		{
			desc: "static mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
				Timeouts: testTimeoutConfig(),
			},
			wantOptions: nil,
		},
		{
			desc: "dynamic mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
				Timeouts: testTimeoutConfig(),
			},
			wantOptions: nil,
		},
		{
			desc: "openshift-logging mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
				Timeouts: testTimeoutConfig(),
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled: true,
					},
				},
			},
			wantOptions: &config.AlertManagerConfig{
				EnableV2:        true,
				EnableDiscovery: true,
				RefreshInterval: "1m",
				Hosts:           "https://_web._tcp.alertmanager-operated.openshift-monitoring.svc",
			},
		},
		{
			desc: "openshift-network mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
				},
				Timeouts: testTimeoutConfig(),
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled: true,
					},
				},
			},
			wantOptions: &config.AlertManagerConfig{
				EnableV2:        true,
				EnableDiscovery: true,
				RefreshInterval: "1m",
				Hosts:           "https://_web._tcp.alertmanager-operated.openshift-monitoring.svc",
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			cfg := ConfigOptions(tc.opts)
			err := ConfigureOptionsForMode(&cfg, tc.opts)

			require.Nil(t, err)
			require.Equal(t, tc.wantOptions, cfg.Ruler.AlertManager)
		})
	}
}

func TestConfigOptions_RulerAlertManager_UserOverride(t *testing.T) {
	tt := []struct {
		desc        string
		opts        Options
		wantOptions *config.AlertManagerConfig
	}{
		{
			desc: "static mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
				Timeouts: testTimeoutConfig(),
			},
			wantOptions: nil,
		},
		{
			desc: "dynamic mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
				Timeouts: testTimeoutConfig(),
			},
			wantOptions: nil,
		},
		{
			desc: "openshift-logging mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
				},
				Timeouts: testTimeoutConfig(),
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							EnableV2: false,
							DiscoverySpec: &lokiv1.AlertManagerDiscoverySpec{
								EnableSRV:       false,
								RefreshInterval: "2m",
							},
							Endpoints: []string{"http://my-alertmanager"},
						},
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled: true,
					},
				},
			},
			wantOptions: &config.AlertManagerConfig{
				EnableV2:        false,
				EnableDiscovery: false,
				RefreshInterval: "2m",
				Hosts:           "http://my-alertmanager",
			},
		},
		{
			desc: "openshift-network mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
				},
				Timeouts: testTimeoutConfig(),
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							EnableV2: false,
							DiscoverySpec: &lokiv1.AlertManagerDiscoverySpec{
								EnableSRV:       false,
								RefreshInterval: "2m",
							},
							Endpoints: []string{"http://my-alertmanager"},
						},
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled: true,
					},
				},
			},
			wantOptions: &config.AlertManagerConfig{
				EnableV2:        false,
				EnableDiscovery: false,
				RefreshInterval: "2m",
				Hosts:           "http://my-alertmanager",
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			cfg := ConfigOptions(tc.opts)
			err := ConfigureOptionsForMode(&cfg, tc.opts)
			require.Nil(t, err)
			require.Equal(t, tc.wantOptions, cfg.Ruler.AlertManager)
		})
	}
}

func TestConfigOptions_RulerOverrides_OCPApplicationTenant(t *testing.T) {
	tt := []struct {
		desc        string
		opts        Options
		wantOptions map[string]config.LokiOverrides
	}{
		{
			desc: "static mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
				Timeouts: testTimeoutConfig(),
			},
			wantOptions: nil,
		},
		{
			desc: "dynamic mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
				Timeouts: testTimeoutConfig(),
			},
			wantOptions: nil,
		},
		{
			desc: "openshift-logging mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
				},
				Timeouts: testTimeoutConfig(),
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							EnableV2: false,
							DiscoverySpec: &lokiv1.AlertManagerDiscoverySpec{
								EnableSRV:       false,
								RefreshInterval: "2m",
							},
							Endpoints: []string{"http://my-alertmanager"},
						},
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled:             true,
						UserWorkloadAlertManagerEnabled: true,
					},
				},
			},
			wantOptions: map[string]config.LokiOverrides{
				"application": {
					Ruler: config.RulerOverrides{
						AlertManager: &config.AlertManagerConfig{
							Hosts:           "https://_web._tcp.alertmanager-operated.openshift-user-workload-monitoring.svc",
							EnableV2:        true,
							EnableDiscovery: true,
							RefreshInterval: "1m",
							Notifier: &config.NotifierConfig{
								TLS: config.TLSConfig{
									ServerName: ptr.To("alertmanager-user-workload.openshift-user-workload-monitoring.svc.cluster.local"),
									CAPath:     ptr.To("/var/run/ca/alertmanager/service-ca.crt"),
								},
								HeaderAuth: config.HeaderAuth{
									Type:            ptr.To("Bearer"),
									CredentialsFile: ptr.To("/var/run/secrets/kubernetes.io/serviceaccount/token"),
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "openshift-network mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
				},
				Timeouts: testTimeoutConfig(),
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							EnableV2: false,
							DiscoverySpec: &lokiv1.AlertManagerDiscoverySpec{
								EnableSRV:       false,
								RefreshInterval: "2m",
							},
							Endpoints: []string{"http://my-alertmanager"},
						},
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled: true,
					},
				},
			},
			wantOptions: nil,
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			cfg := ConfigOptions(tc.opts)
			err := ConfigureOptionsForMode(&cfg, tc.opts)
			require.Nil(t, err)
			require.EqualValues(t, tc.wantOptions, cfg.Overrides)
		})
	}
}

func TestConfigOptions_RulerOverrides(t *testing.T) {
	tt := []struct {
		desc        string
		opts        Options
		wantOptions map[string]config.LokiOverrides
	}{
		{
			desc: "static mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
				Timeouts: testTimeoutConfig(),
			},
			wantOptions: nil,
		},
		{
			desc: "dynamic mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
				Timeouts: testTimeoutConfig(),
			},
			wantOptions: nil,
		},
		{
			desc: "openshift-logging mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
				},
				Timeouts: testTimeoutConfig(),
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							EnableV2: false,
							DiscoverySpec: &lokiv1.AlertManagerDiscoverySpec{
								EnableSRV:       false,
								RefreshInterval: "2m",
							},
							Endpoints: []string{"http://my-alertmanager"},
						},
						Overrides: map[string]lokiv1.RulerOverrides{
							"application": {
								AlertManagerOverrides: &lokiv1.AlertManagerSpec{
									ExternalURL:    "external",
									ExternalLabels: map[string]string{"external": "label"},
									EnableV2:       false,
									Endpoints:      []string{"http://application-alertmanager"},
									DiscoverySpec: &lokiv1.AlertManagerDiscoverySpec{
										EnableSRV:       false,
										RefreshInterval: "3m",
									},
									Client: &lokiv1.AlertManagerClientConfig{
										TLS: &lokiv1.AlertManagerClientTLSConfig{
											ServerName: ptr.To("application.svc"),
											CAPath:     ptr.To("/tenant/application/alertmanager/ca.crt"),
											CertPath:   ptr.To("/tenant/application/alertmanager/cert.crt"),
											KeyPath:    ptr.To("/tenant/application/alertmanager/cert.key"),
										},
										HeaderAuth: &lokiv1.AlertManagerClientHeaderAuth{
											Type:        ptr.To("Bearer"),
											Credentials: ptr.To("letmeinplz"),
										},
									},
								},
							},
							"other-tenant": {
								AlertManagerOverrides: &lokiv1.AlertManagerSpec{
									ExternalURL:    "external1",
									ExternalLabels: map[string]string{"external1": "label1"},
									EnableV2:       false,
									Endpoints:      []string{"http://other-alertmanager"},
									DiscoverySpec: &lokiv1.AlertManagerDiscoverySpec{
										EnableSRV:       true,
										RefreshInterval: "5m",
									},
									Client: &lokiv1.AlertManagerClientConfig{
										TLS: &lokiv1.AlertManagerClientTLSConfig{
											ServerName: ptr.To("other.svc"),
											CAPath:     ptr.To("/tenant/other/alertmanager/ca.crt"),
											CertPath:   ptr.To("/tenant/other/alertmanager/cert.crt"),
											KeyPath:    ptr.To("/tenant/other/alertmanager/cert.key"),
										},
										BasicAuth: &lokiv1.AlertManagerClientBasicAuth{
											Username: ptr.To("user"),
											Password: ptr.To("pass"),
										},
									},
								},
							},
						},
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled:             true,
						UserWorkloadAlertManagerEnabled: true,
					},
				},
			},
			wantOptions: map[string]config.LokiOverrides{
				"application": {
					Ruler: config.RulerOverrides{
						AlertManager: &config.AlertManagerConfig{
							ExternalURL:     "external",
							Hosts:           "http://application-alertmanager",
							EnableV2:        false,
							EnableDiscovery: false,
							RefreshInterval: "3m",
							ExternalLabels:  map[string]string{"external": "label"},
							Notifier: &config.NotifierConfig{
								TLS: config.TLSConfig{
									ServerName: ptr.To("application.svc"),
									CAPath:     ptr.To("/tenant/application/alertmanager/ca.crt"),
									CertPath:   ptr.To("/tenant/application/alertmanager/cert.crt"),
									KeyPath:    ptr.To("/tenant/application/alertmanager/cert.key"),
								},
								HeaderAuth: config.HeaderAuth{
									Type:        ptr.To("Bearer"),
									Credentials: ptr.To("letmeinplz"),
								},
							},
						},
					},
				},
				"other-tenant": {
					Ruler: config.RulerOverrides{
						AlertManager: &config.AlertManagerConfig{
							ExternalURL:     "external1",
							Hosts:           "http://other-alertmanager",
							EnableV2:        false,
							EnableDiscovery: true,
							RefreshInterval: "5m",
							ExternalLabels:  map[string]string{"external1": "label1"},
							Notifier: &config.NotifierConfig{
								TLS: config.TLSConfig{
									ServerName: ptr.To("other.svc"),
									CAPath:     ptr.To("/tenant/other/alertmanager/ca.crt"),
									CertPath:   ptr.To("/tenant/other/alertmanager/cert.crt"),
									KeyPath:    ptr.To("/tenant/other/alertmanager/cert.key"),
								},
								BasicAuth: config.BasicAuth{
									Username: ptr.To("user"),
									Password: ptr.To("pass"),
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "openshift-network mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
				},
				Timeouts: testTimeoutConfig(),
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							EnableV2: false,
							DiscoverySpec: &lokiv1.AlertManagerDiscoverySpec{
								EnableSRV:       false,
								RefreshInterval: "2m",
							},
							Endpoints: []string{"http://my-alertmanager"},
						},
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled: true,
					},
				},
			},
			wantOptions: nil,
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			cfg := ConfigOptions(tc.opts)
			err := ConfigureOptionsForMode(&cfg, tc.opts)
			require.Nil(t, err)
			require.EqualValues(t, tc.wantOptions, cfg.Overrides)
		})
	}
}

func TestConfigOptions_RulerOverrides_OCPUserWorkloadOnlyEnabled(t *testing.T) {
	tt := []struct {
		desc                 string
		opts                 Options
		wantOptions          *config.AlertManagerConfig
		wantOverridesOptions map[string]config.LokiOverrides
	}{
		{
			desc: "static mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Static,
					},
				},
				Timeouts: testTimeoutConfig(),
			},
			wantOptions:          nil,
			wantOverridesOptions: nil,
		},
		{
			desc: "dynamic mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.Dynamic,
					},
				},
				Timeouts: testTimeoutConfig(),
			},
			wantOptions:          nil,
			wantOverridesOptions: nil,
		},
		{
			desc: "openshift-logging mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
				},
				Timeouts: testTimeoutConfig(),
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							EnableV2: false,
							DiscoverySpec: &lokiv1.AlertManagerDiscoverySpec{
								EnableSRV:       false,
								RefreshInterval: "2m",
							},
							Endpoints: []string{"http://my-alertmanager"},
						},
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled:             false,
						UserWorkloadAlertManagerEnabled: true,
					},
				},
			},
			wantOptions: &config.AlertManagerConfig{
				EnableV2:        false,
				EnableDiscovery: false,
				RefreshInterval: "2m",
				Hosts:           "http://my-alertmanager",
			},
			wantOverridesOptions: map[string]config.LokiOverrides{
				"application": {
					Ruler: config.RulerOverrides{
						AlertManager: &config.AlertManagerConfig{
							Hosts:           "https://_web._tcp.alertmanager-operated.openshift-user-workload-monitoring.svc",
							EnableV2:        true,
							EnableDiscovery: true,
							RefreshInterval: "1m",
							Notifier: &config.NotifierConfig{
								TLS: config.TLSConfig{
									ServerName: ptr.To("alertmanager-user-workload.openshift-user-workload-monitoring.svc.cluster.local"),
									CAPath:     ptr.To("/var/run/ca/alertmanager/service-ca.crt"),
								},
								HeaderAuth: config.HeaderAuth{
									Type:            ptr.To("Bearer"),
									CredentialsFile: ptr.To("/var/run/secrets/kubernetes.io/serviceaccount/token"),
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "openshift-logging mode with application override",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
					Limits: &lokiv1.LimitsSpec{
						Tenants: map[string]lokiv1.PerTenantLimitsTemplateSpec{
							"application": {
								QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
									QueryLimitSpec: lokiv1.QueryLimitSpec{
										QueryTimeout: "5m",
									},
								},
							},
						},
					},
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftLogging,
					},
				},
				Timeouts: testTimeoutConfig(),
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							EnableV2: false,
							DiscoverySpec: &lokiv1.AlertManagerDiscoverySpec{
								EnableSRV:       false,
								RefreshInterval: "2m",
							},
							Endpoints: []string{"http://my-alertmanager"},
						},
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled:             false,
						UserWorkloadAlertManagerEnabled: true,
					},
				},
			},
			wantOptions: &config.AlertManagerConfig{
				EnableV2:        false,
				EnableDiscovery: false,
				RefreshInterval: "2m",
				Hosts:           "http://my-alertmanager",
			},
			wantOverridesOptions: map[string]config.LokiOverrides{
				"application": {
					Limits: lokiv1.PerTenantLimitsTemplateSpec{
						QueryLimits: &lokiv1.PerTenantQueryLimitSpec{
							QueryLimitSpec: lokiv1.QueryLimitSpec{
								QueryTimeout: "5m",
							},
						},
					},
					Ruler: config.RulerOverrides{
						AlertManager: &config.AlertManagerConfig{
							Hosts:           "https://_web._tcp.alertmanager-operated.openshift-user-workload-monitoring.svc",
							EnableV2:        true,
							EnableDiscovery: true,
							RefreshInterval: "1m",
							Notifier: &config.NotifierConfig{
								TLS: config.TLSConfig{
									ServerName: ptr.To("alertmanager-user-workload.openshift-user-workload-monitoring.svc.cluster.local"),
									CAPath:     ptr.To("/var/run/ca/alertmanager/service-ca.crt"),
								},
								HeaderAuth: config.HeaderAuth{
									Type:            ptr.To("Bearer"),
									CredentialsFile: ptr.To("/var/run/secrets/kubernetes.io/serviceaccount/token"),
								},
							},
						},
					},
				},
			},
		},
		{
			desc: "openshift-network mode",
			opts: Options{
				Stack: lokiv1.LokiStackSpec{
					Tenants: &lokiv1.TenantsSpec{
						Mode: lokiv1.OpenshiftNetwork,
					},
					Rules: &lokiv1.RulesSpec{
						Enabled: true,
					},
				},

				Timeouts: testTimeoutConfig(),
				Ruler: Ruler{
					Spec: &lokiv1.RulerConfigSpec{
						AlertManagerSpec: &lokiv1.AlertManagerSpec{
							EnableV2: false,
							DiscoverySpec: &lokiv1.AlertManagerDiscoverySpec{
								EnableSRV:       false,
								RefreshInterval: "2m",
							},
							Endpoints: []string{"http://my-alertmanager"},
						},
					},
				},
				OpenShiftOptions: openshift.Options{
					BuildOpts: openshift.BuildOptions{
						AlertManagerEnabled: false,
					},
				},
			},
			wantOptions: &config.AlertManagerConfig{
				EnableV2:        false,
				EnableDiscovery: false,
				RefreshInterval: "2m",
				Hosts:           "http://my-alertmanager",
			},
			wantOverridesOptions: nil,
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			cfg := ConfigOptions(tc.opts)
			err := ConfigureOptionsForMode(&cfg, tc.opts)
			require.Nil(t, err)
			require.EqualValues(t, tc.wantOverridesOptions, cfg.Overrides)
			require.EqualValues(t, tc.wantOptions, cfg.Ruler.AlertManager)
		})
	}
}

func TestConfigOptions_Replication(t *testing.T) {
	tt := []struct {
		desc        string
		spec        lokiv1.LokiStackSpec
		wantOptions lokiv1.ReplicationSpec
	}{
		{
			desc: "nominal case",
			spec: lokiv1.LokiStackSpec{
				Replication: &lokiv1.ReplicationSpec{
					Factor: 2,
					Zones: []lokiv1.ZoneSpec{
						{
							TopologyKey: "us-east-1a",
							MaxSkew:     1,
						},
						{
							TopologyKey: "us-east-1b",
							MaxSkew:     2,
						},
					},
				},
			},
			wantOptions: lokiv1.ReplicationSpec{
				Factor: 2,
				Zones: []lokiv1.ZoneSpec{
					{
						TopologyKey: "us-east-1a",
						MaxSkew:     1,
					},
					{
						TopologyKey: "us-east-1b",
						MaxSkew:     2,
					},
				},
			},
		},
		{
			desc: "using deprecated ReplicationFactor",
			spec: lokiv1.LokiStackSpec{
				ReplicationFactor: 3,
			},
			wantOptions: lokiv1.ReplicationSpec{
				Factor: 3,
			},
		},
		{
			desc: "using deprecated ReplicationFactor with ReplicationSpec",
			spec: lokiv1.LokiStackSpec{
				ReplicationFactor: 2,
				Replication: &lokiv1.ReplicationSpec{
					Factor: 4,
					Zones: []lokiv1.ZoneSpec{
						{
							TopologyKey: "us-east-1a",
							MaxSkew:     1,
						},
						{
							TopologyKey: "us-east-1b",
							MaxSkew:     2,
						},
					},
				},
			},
			wantOptions: lokiv1.ReplicationSpec{
				Factor: 4,
				Zones: []lokiv1.ZoneSpec{
					{
						TopologyKey: "us-east-1a",
						MaxSkew:     1,
					},
					{
						TopologyKey: "us-east-1b",
						MaxSkew:     2,
					},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			inOpt := Options{
				Stack:    tc.spec,
				Timeouts: testTimeoutConfig(),
			}
			options := ConfigOptions(inOpt)
			require.Equal(t, tc.wantOptions, *options.Stack.Replication)
		})
	}
}

func TestConfigOptions_ServerOptions(t *testing.T) {
	opt := Options{
		Stack:    lokiv1.LokiStackSpec{},
		Timeouts: testTimeoutConfig(),
	}
	got := ConfigOptions(opt)

	want := config.HTTPTimeoutConfig{
		IdleTimeout:  time.Second,
		ReadTimeout:  time.Minute,
		WriteTimeout: 10 * time.Minute,
	}

	require.Equal(t, want, got.HTTPTimeouts)
}

func TestConfigOptions_Shipper(t *testing.T) {
	for _, tc := range []struct {
		name        string
		inOpt       Options
		wantShipper []string
	}{
		{
			name: "default_config_v11_schema",
			inOpt: Options{
				Stack: lokiv1.LokiStackSpec{
					Storage: lokiv1.ObjectStorageSpec{
						Schemas: []lokiv1.ObjectStorageSchema{
							{
								Version:       lokiv1.ObjectStorageSchemaV11,
								EffectiveDate: "2020-10-01",
							},
						},
					},
				},
			},
			wantShipper: []string{"boltdb"},
		},
		{
			name: "v12_schema",
			inOpt: Options{
				Stack: lokiv1.LokiStackSpec{
					Storage: lokiv1.ObjectStorageSpec{
						Schemas: []lokiv1.ObjectStorageSchema{
							{
								Version:       lokiv1.ObjectStorageSchemaV12,
								EffectiveDate: "2020-02-05",
							},
						},
					},
				},
			},
			wantShipper: []string{"boltdb"},
		},
		{
			name: "v13_schema",
			inOpt: Options{
				Stack: lokiv1.LokiStackSpec{
					Storage: lokiv1.ObjectStorageSpec{
						Schemas: []lokiv1.ObjectStorageSchema{
							{
								Version:       lokiv1.ObjectStorageSchemaV13,
								EffectiveDate: "2024-01-01",
							},
						},
					},
				},
			},
			wantShipper: []string{"tsdb"},
		},
		{
			name: "multiple_schema",
			inOpt: Options{
				Stack: lokiv1.LokiStackSpec{
					Storage: lokiv1.ObjectStorageSpec{
						Schemas: []lokiv1.ObjectStorageSchema{
							{
								Version:       lokiv1.ObjectStorageSchemaV11,
								EffectiveDate: "2020-01-01",
							},
							{
								Version:       lokiv1.ObjectStorageSchemaV12,
								EffectiveDate: "2021-01-01",
							},
							{
								Version:       lokiv1.ObjectStorageSchemaV13,
								EffectiveDate: "2024-01-01",
							},
						},
					},
				},
			},
			wantShipper: []string{"boltdb", "tsdb"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := ConfigOptions(tc.inOpt)
			require.Equal(t, tc.wantShipper, got.Shippers)
		})
	}
}
