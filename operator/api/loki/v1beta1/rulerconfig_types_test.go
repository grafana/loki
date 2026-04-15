package v1beta1_test

import (
	"testing"

	v1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/api/loki/v1beta1"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func TestConvertToV1_RulerConfig(t *testing.T) {
	tt := []struct {
		desc string
		src  v1beta1.RulerConfig
		want v1.RulerConfig
	}{
		{
			desc: "empty src(v1beta1) and dst(v1) RulerConfig",
			src:  v1beta1.RulerConfig{},
			want: v1.RulerConfig{},
		},
		{
			desc: "full conversion of src(v1beta1) to dst(v1) RulerConfig",
			src: v1beta1.RulerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lokistack-dev",
					Namespace: "jupiter",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1beta1.RulerConfigSpec{
					EvalutionInterval: "1m",
					PollInterval:      "2m",
					AlertManagerSpec: &v1beta1.AlertManagerSpec{
						ExternalURL: "am.io",
						ExternalLabels: map[string]string{
							"foo": "bar",
							"tik": "tok",
						},
						EnableV2:  true,
						Endpoints: []string{"endpoint-1", "endpoint-2"},
						DiscoverySpec: &v1beta1.AlertManagerDiscoverySpec{
							EnableSRV:       true,
							RefreshInterval: "3m",
						},
						NotificationQueueSpec: &v1beta1.AlertManagerNotificationQueueSpec{
							Capacity:           500,
							Timeout:            v1beta1.PrometheusDuration("5s"),
							ForOutageTolerance: v1beta1.PrometheusDuration("10m"),
							ForGracePeriod:     v1beta1.PrometheusDuration("10s"),
							ResendDelay:        "1s",
						},
						RelabelConfigs: []v1beta1.RelabelConfig{
							{
								SourceLabels: []string{"l1", "l2"},
								Separator:    "|",
								TargetLabel:  "new-label",
								Regex:        "*.",
								Modulus:      4,
								Replacement:  "$2",
								Action:       "replace",
							},
							{
								SourceLabels: []string{"l21", "l22"},
								Separator:    ",",
								TargetLabel:  "newer-label",
								Regex:        "^[a-zA-Z-]",
								Modulus:      42,
								Replacement:  "$3",
								Action:       "drop",
							},
						},
						Client: &v1beta1.AlertManagerClientConfig{
							TLS: &v1beta1.AlertManagerClientTLSConfig{
								CAPath:     ptr.To("/tls/ca/path"),
								ServerName: ptr.To("server"),
								CertPath:   ptr.To("/tls/cert/path"),
								KeyPath:    ptr.To("/tls/key/path"),
							},
							HeaderAuth: &v1beta1.AlertManagerClientHeaderAuth{
								Type:            ptr.To("type"),
								Credentials:     ptr.To("creds"),
								CredentialsFile: ptr.To("creds-file"),
							},
							BasicAuth: &v1beta1.AlertManagerClientBasicAuth{
								Username: ptr.To("user"),
								Password: ptr.To("pass"),
							},
						},
					},
					RemoteWriteSpec: &v1beta1.RemoteWriteSpec{
						Enabled:       true,
						RefreshPeriod: "7m",
						ClientSpec: &v1beta1.RemoteWriteClientSpec{
							Name:                    "remote",
							URL:                     "remote.fr",
							Timeout:                 "8s",
							AuthorizationType:       v1beta1.BearerAuthorization,
							AuthorizationSecretName: "secret",
							AdditionalHeaders: map[string]string{
								"flip": "flop",
								"tic":  "tac",
							},
							RelabelConfigs: []v1beta1.RelabelConfig{
								{
									SourceLabels: []string{"rl1", "rl2"},
									Separator:    ";",
									TargetLabel:  "new-relabel",
									Regex:        "^[1-9]",
									Modulus:      711,
									Replacement:  "$6",
									Action:       "keep",
								},
								{
									SourceLabels: []string{"rl21", "rl22"},
									Separator:    "/",
									TargetLabel:  "newer-relabel",
									Regex:        "^[A-Z-]",
									Modulus:      117,
									Replacement:  "$3",
									Action:       "hashmod",
								},
							},
							ProxyURL:        "proxy",
							FollowRedirects: true,
						},
					},
					Overrides: map[string]v1beta1.RulerOverrides{
						"tenant-1": {
							AlertManagerOverrides: &v1beta1.AlertManagerSpec{
								ExternalURL: "external.io",
								ExternalLabels: map[string]string{
									"dink": "doink",
									"yin":  "yang",
								},
								EnableV2:  true,
								Endpoints: []string{"t-ep-1", "t-ep-2"},
								DiscoverySpec: &v1beta1.AlertManagerDiscoverySpec{
									EnableSRV:       true,
									RefreshInterval: "42s",
								},
								NotificationQueueSpec: &v1beta1.AlertManagerNotificationQueueSpec{
									Capacity:           350,
									Timeout:            v1beta1.PrometheusDuration("5m"),
									ForOutageTolerance: v1beta1.PrometheusDuration("10s"),
									ForGracePeriod:     v1beta1.PrometheusDuration("10m"),
									ResendDelay:        "2s",
								},
								RelabelConfigs: []v1beta1.RelabelConfig{
									{
										SourceLabels: []string{"r2l1", "r2l2"},
										Separator:    "-",
										TargetLabel:  "new-relabel-1",
										Regex:        "^[0-7]",
										Modulus:      71,
										Replacement:  "$9",
										Action:       "labeldrop",
									},
									{
										SourceLabels: []string{"r2l21", "r2l22"},
										Separator:    "%",
										TargetLabel:  "new-relabel-2",
										Regex:        "^[a-z]",
										Modulus:      56,
										Replacement:  "$4",
										Action:       "labelkeep",
									},
								},
								Client: &v1beta1.AlertManagerClientConfig{
									TLS: &v1beta1.AlertManagerClientTLSConfig{
										CAPath:     ptr.To("/tls/ca/path-1"),
										ServerName: ptr.To("server-1"),
										CertPath:   ptr.To("/tls/cert/path-1"),
										KeyPath:    ptr.To("/tls/key/path-1"),
									},
									HeaderAuth: &v1beta1.AlertManagerClientHeaderAuth{
										Type:            ptr.To("type-1"),
										Credentials:     ptr.To("creds-1"),
										CredentialsFile: ptr.To("creds-file-1"),
									},
									BasicAuth: &v1beta1.AlertManagerClientBasicAuth{
										Username: ptr.To("user-1"),
										Password: ptr.To("pass-1"),
									},
								},
							},
						},
						"tenant-2": {
							AlertManagerOverrides: &v1beta1.AlertManagerSpec{
								ExternalURL: "external-2.io",
								ExternalLabels: map[string]string{
									"dink-2": "doink-2",
									"yin-2":  "yang-2",
								},
								EnableV2:  false,
								Endpoints: []string{"t-2-ep-1", "t-2-ep-2"},
								DiscoverySpec: &v1beta1.AlertManagerDiscoverySpec{
									EnableSRV:       false,
									RefreshInterval: "43s",
								},
								NotificationQueueSpec: &v1beta1.AlertManagerNotificationQueueSpec{
									Capacity:           300,
									Timeout:            v1beta1.PrometheusDuration("4m"),
									ForOutageTolerance: v1beta1.PrometheusDuration("11s"),
									ForGracePeriod:     v1beta1.PrometheusDuration("9m"),
									ResendDelay:        "3s",
								},
								RelabelConfigs: []v1beta1.RelabelConfig{
									{
										SourceLabels: []string{"r2l1-2", "r2l2-2"},
										Separator:    ".",
										TargetLabel:  "new-relabel-21",
										Regex:        "^[1-8]",
										Modulus:      70,
										Replacement:  "$3",
										Action:       "labelkeep",
									},
									{
										SourceLabels: []string{"r2l21-2", "r2l22-2"},
										Separator:    ":",
										TargetLabel:  "new-relabel-22",
										Regex:        "[a-zA-Z0-9_]",
										Modulus:      87,
										Replacement:  "$5",
										Action:       "replace",
									},
								},
								Client: &v1beta1.AlertManagerClientConfig{
									TLS: &v1beta1.AlertManagerClientTLSConfig{
										CAPath:     ptr.To("/tls/ca/path-1"),
										ServerName: ptr.To("server-1"),
										CertPath:   ptr.To("/tls/cert/path-1"),
										KeyPath:    ptr.To("/tls/key/path-1"),
									},
									HeaderAuth: &v1beta1.AlertManagerClientHeaderAuth{
										Type:            ptr.To("type-1"),
										Credentials:     ptr.To("creds-1"),
										CredentialsFile: ptr.To("creds-file-1"),
									},
									BasicAuth: &v1beta1.AlertManagerClientBasicAuth{
										Username: ptr.To("user-1"),
										Password: ptr.To("pass-1"),
									},
								},
							},
						},
					},
				},
				Status: v1beta1.RulerConfigStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(v1.ConditionReady),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(v1.ConditionPending),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			want: v1.RulerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lokistack-dev",
					Namespace: "jupiter",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1.RulerConfigSpec{
					EvalutionInterval: "1m",
					PollInterval:      "2m",
					AlertManagerSpec: &v1.AlertManagerSpec{
						ExternalURL: "am.io",
						ExternalLabels: map[string]string{
							"foo": "bar",
							"tik": "tok",
						},
						EnableV2:  true,
						Endpoints: []string{"endpoint-1", "endpoint-2"},
						DiscoverySpec: &v1.AlertManagerDiscoverySpec{
							EnableSRV:       true,
							RefreshInterval: "3m",
						},
						NotificationQueueSpec: &v1.AlertManagerNotificationQueueSpec{
							Capacity:           500,
							Timeout:            v1.PrometheusDuration("5s"),
							ForOutageTolerance: v1.PrometheusDuration("10m"),
							ForGracePeriod:     v1.PrometheusDuration("10s"),
							ResendDelay:        "1s",
						},
						RelabelConfigs: []v1.RelabelConfig{
							{
								SourceLabels: []string{"l1", "l2"},
								Separator:    "|",
								TargetLabel:  "new-label",
								Regex:        "*.",
								Modulus:      4,
								Replacement:  "$2",
								Action:       "replace",
							},
							{
								SourceLabels: []string{"l21", "l22"},
								Separator:    ",",
								TargetLabel:  "newer-label",
								Regex:        "^[a-zA-Z-]",
								Modulus:      42,
								Replacement:  "$3",
								Action:       "drop",
							},
						},
						Client: &v1.AlertManagerClientConfig{
							TLS: &v1.AlertManagerClientTLSConfig{
								CAPath:     ptr.To("/tls/ca/path"),
								ServerName: ptr.To("server"),
								CertPath:   ptr.To("/tls/cert/path"),
								KeyPath:    ptr.To("/tls/key/path"),
							},
							HeaderAuth: &v1.AlertManagerClientHeaderAuth{
								Type:            ptr.To("type"),
								Credentials:     ptr.To("creds"),
								CredentialsFile: ptr.To("creds-file"),
							},
							BasicAuth: &v1.AlertManagerClientBasicAuth{
								Username: ptr.To("user"),
								Password: ptr.To("pass"),
							},
						},
					},
					RemoteWriteSpec: &v1.RemoteWriteSpec{
						Enabled:       true,
						RefreshPeriod: "7m",
						ClientSpec: &v1.RemoteWriteClientSpec{
							Name:                    "remote",
							URL:                     "remote.fr",
							Timeout:                 "8s",
							AuthorizationType:       v1.BearerAuthorization,
							AuthorizationSecretName: "secret",
							AdditionalHeaders: map[string]string{
								"flip": "flop",
								"tic":  "tac",
							},
							RelabelConfigs: []v1.RelabelConfig{
								{
									SourceLabels: []string{"rl1", "rl2"},
									Separator:    ";",
									TargetLabel:  "new-relabel",
									Regex:        "^[1-9]",
									Modulus:      711,
									Replacement:  "$6",
									Action:       "keep",
								},
								{
									SourceLabels: []string{"rl21", "rl22"},
									Separator:    "/",
									TargetLabel:  "newer-relabel",
									Regex:        "^[A-Z-]",
									Modulus:      117,
									Replacement:  "$3",
									Action:       "hashmod",
								},
							},
							ProxyURL:        "proxy",
							FollowRedirects: true,
						},
					},
					Overrides: map[string]v1.RulerOverrides{
						"tenant-1": {
							AlertManagerOverrides: &v1.AlertManagerSpec{
								ExternalURL: "external.io",
								ExternalLabels: map[string]string{
									"dink": "doink",
									"yin":  "yang",
								},
								EnableV2:  true,
								Endpoints: []string{"t-ep-1", "t-ep-2"},
								DiscoverySpec: &v1.AlertManagerDiscoverySpec{
									EnableSRV:       true,
									RefreshInterval: "42s",
								},
								NotificationQueueSpec: &v1.AlertManagerNotificationQueueSpec{
									Capacity:           350,
									Timeout:            v1.PrometheusDuration("5m"),
									ForOutageTolerance: v1.PrometheusDuration("10s"),
									ForGracePeriod:     v1.PrometheusDuration("10m"),
									ResendDelay:        "2s",
								},
								RelabelConfigs: []v1.RelabelConfig{
									{
										SourceLabels: []string{"r2l1", "r2l2"},
										Separator:    "-",
										TargetLabel:  "new-relabel-1",
										Regex:        "^[0-7]",
										Modulus:      71,
										Replacement:  "$9",
										Action:       "labeldrop",
									},
									{
										SourceLabels: []string{"r2l21", "r2l22"},
										Separator:    "%",
										TargetLabel:  "new-relabel-2",
										Regex:        "^[a-z]",
										Modulus:      56,
										Replacement:  "$4",
										Action:       "labelkeep",
									},
								},
								Client: &v1.AlertManagerClientConfig{
									TLS: &v1.AlertManagerClientTLSConfig{
										CAPath:     ptr.To("/tls/ca/path-1"),
										ServerName: ptr.To("server-1"),
										CertPath:   ptr.To("/tls/cert/path-1"),
										KeyPath:    ptr.To("/tls/key/path-1"),
									},
									HeaderAuth: &v1.AlertManagerClientHeaderAuth{
										Type:            ptr.To("type-1"),
										Credentials:     ptr.To("creds-1"),
										CredentialsFile: ptr.To("creds-file-1"),
									},
									BasicAuth: &v1.AlertManagerClientBasicAuth{
										Username: ptr.To("user-1"),
										Password: ptr.To("pass-1"),
									},
								},
							},
						},
						"tenant-2": {
							AlertManagerOverrides: &v1.AlertManagerSpec{
								ExternalURL: "external-2.io",
								ExternalLabels: map[string]string{
									"dink-2": "doink-2",
									"yin-2":  "yang-2",
								},
								EnableV2:  false,
								Endpoints: []string{"t-2-ep-1", "t-2-ep-2"},
								DiscoverySpec: &v1.AlertManagerDiscoverySpec{
									EnableSRV:       false,
									RefreshInterval: "43s",
								},
								NotificationQueueSpec: &v1.AlertManagerNotificationQueueSpec{
									Capacity:           300,
									Timeout:            v1.PrometheusDuration("4m"),
									ForOutageTolerance: v1.PrometheusDuration("11s"),
									ForGracePeriod:     v1.PrometheusDuration("9m"),
									ResendDelay:        "3s",
								},
								RelabelConfigs: []v1.RelabelConfig{
									{
										SourceLabels: []string{"r2l1-2", "r2l2-2"},
										Separator:    ".",
										TargetLabel:  "new-relabel-21",
										Regex:        "^[1-8]",
										Modulus:      70,
										Replacement:  "$3",
										Action:       "labelkeep",
									},
									{
										SourceLabels: []string{"r2l21-2", "r2l22-2"},
										Separator:    ":",
										TargetLabel:  "new-relabel-22",
										Regex:        "[a-zA-Z0-9_]",
										Modulus:      87,
										Replacement:  "$5",
										Action:       "replace",
									},
								},
								Client: &v1.AlertManagerClientConfig{
									TLS: &v1.AlertManagerClientTLSConfig{
										CAPath:     ptr.To("/tls/ca/path-1"),
										ServerName: ptr.To("server-1"),
										CertPath:   ptr.To("/tls/cert/path-1"),
										KeyPath:    ptr.To("/tls/key/path-1"),
									},
									HeaderAuth: &v1.AlertManagerClientHeaderAuth{
										Type:            ptr.To("type-1"),
										Credentials:     ptr.To("creds-1"),
										CredentialsFile: ptr.To("creds-file-1"),
									},
									BasicAuth: &v1.AlertManagerClientBasicAuth{
										Username: ptr.To("user-1"),
										Password: ptr.To("pass-1"),
									},
								},
							},
						},
					},
				},
				Status: v1.RulerConfigStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(v1.ConditionReady),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(v1.ConditionPending),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			dst := v1.RulerConfig{}
			err := tc.src.ConvertTo(&dst)
			require.NoError(t, err)
			require.Equal(t, dst, tc.want)
		})
	}
}

func TestConvertFromV1_RulerConfig(t *testing.T) {
	tt := []struct {
		desc string
		src  v1.RulerConfig
		want v1beta1.RulerConfig
	}{
		{
			desc: "empty src(v1) and dst(v1beta1) RulerConfig",
			src:  v1.RulerConfig{},
			want: v1beta1.RulerConfig{},
		},
		{
			desc: "full conversion of src(v1) to dst(v1beta1) RulerConfig",
			src: v1.RulerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lokistack-dev",
					Namespace: "jupiter",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1.RulerConfigSpec{
					EvalutionInterval: "1m",
					PollInterval:      "2m",
					AlertManagerSpec: &v1.AlertManagerSpec{
						ExternalURL: "am.io",
						ExternalLabels: map[string]string{
							"foo": "bar",
							"tik": "tok",
						},
						EnableV2:  true,
						Endpoints: []string{"endpoint-1", "endpoint-2"},
						DiscoverySpec: &v1.AlertManagerDiscoverySpec{
							EnableSRV:       true,
							RefreshInterval: "3m",
						},
						NotificationQueueSpec: &v1.AlertManagerNotificationQueueSpec{
							Capacity:           500,
							Timeout:            v1.PrometheusDuration("5s"),
							ForOutageTolerance: v1.PrometheusDuration("10m"),
							ForGracePeriod:     v1.PrometheusDuration("10s"),
							ResendDelay:        "1s",
						},
						RelabelConfigs: []v1.RelabelConfig{
							{
								SourceLabels: []string{"l1", "l2"},
								Separator:    "|",
								TargetLabel:  "new-label",
								Regex:        "*.",
								Modulus:      4,
								Replacement:  "$2",
								Action:       "replace",
							},
							{
								SourceLabels: []string{"l21", "l22"},
								Separator:    ",",
								TargetLabel:  "newer-label",
								Regex:        "^[a-zA-Z-]",
								Modulus:      42,
								Replacement:  "$3",
								Action:       "drop",
							},
						},
						Client: &v1.AlertManagerClientConfig{
							TLS: &v1.AlertManagerClientTLSConfig{
								CAPath:     ptr.To("/tls/ca/path"),
								ServerName: ptr.To("server"),
								CertPath:   ptr.To("/tls/cert/path"),
								KeyPath:    ptr.To("/tls/key/path"),
							},
							HeaderAuth: &v1.AlertManagerClientHeaderAuth{
								Type:            ptr.To("type"),
								Credentials:     ptr.To("creds"),
								CredentialsFile: ptr.To("creds-file"),
							},
							BasicAuth: &v1.AlertManagerClientBasicAuth{
								Username: ptr.To("user"),
								Password: ptr.To("pass"),
							},
						},
					},
					RemoteWriteSpec: &v1.RemoteWriteSpec{
						Enabled:       true,
						RefreshPeriod: "7m",
						ClientSpec: &v1.RemoteWriteClientSpec{
							Name:                    "remote",
							URL:                     "remote.fr",
							Timeout:                 "8s",
							AuthorizationType:       v1.BearerAuthorization,
							AuthorizationSecretName: "secret",
							AdditionalHeaders: map[string]string{
								"flip": "flop",
								"tic":  "tac",
							},
							RelabelConfigs: []v1.RelabelConfig{
								{
									SourceLabels: []string{"rl1", "rl2"},
									Separator:    ";",
									TargetLabel:  "new-relabel",
									Regex:        "^[1-9]",
									Modulus:      711,
									Replacement:  "$6",
									Action:       "keep",
								},
								{
									SourceLabels: []string{"rl21", "rl22"},
									Separator:    "/",
									TargetLabel:  "newer-relabel",
									Regex:        "^[A-Z-]",
									Modulus:      117,
									Replacement:  "$3",
									Action:       "hashmod",
								},
							},
							ProxyURL:        "proxy",
							FollowRedirects: true,
						},
					},
					Overrides: map[string]v1.RulerOverrides{
						"tenant-1": {
							AlertManagerOverrides: &v1.AlertManagerSpec{
								ExternalURL: "external.io",
								ExternalLabels: map[string]string{
									"dink": "doink",
									"yin":  "yang",
								},
								EnableV2:  true,
								Endpoints: []string{"t-ep-1", "t-ep-2"},
								DiscoverySpec: &v1.AlertManagerDiscoverySpec{
									EnableSRV:       true,
									RefreshInterval: "42s",
								},
								NotificationQueueSpec: &v1.AlertManagerNotificationQueueSpec{
									Capacity:           350,
									Timeout:            v1.PrometheusDuration("5m"),
									ForOutageTolerance: v1.PrometheusDuration("10s"),
									ForGracePeriod:     v1.PrometheusDuration("10m"),
									ResendDelay:        "2s",
								},
								RelabelConfigs: []v1.RelabelConfig{
									{
										SourceLabels: []string{"r2l1", "r2l2"},
										Separator:    "-",
										TargetLabel:  "new-relabel-1",
										Regex:        "^[0-7]",
										Modulus:      71,
										Replacement:  "$9",
										Action:       "labeldrop",
									},
									{
										SourceLabels: []string{"r2l21", "r2l22"},
										Separator:    "%",
										TargetLabel:  "new-relabel-2",
										Regex:        "^[a-z]",
										Modulus:      56,
										Replacement:  "$4",
										Action:       "labelkeep",
									},
								},
								Client: &v1.AlertManagerClientConfig{
									TLS: &v1.AlertManagerClientTLSConfig{
										CAPath:     ptr.To("/tls/ca/path-1"),
										ServerName: ptr.To("server-1"),
										CertPath:   ptr.To("/tls/cert/path-1"),
										KeyPath:    ptr.To("/tls/key/path-1"),
									},
									HeaderAuth: &v1.AlertManagerClientHeaderAuth{
										Type:            ptr.To("type-1"),
										Credentials:     ptr.To("creds-1"),
										CredentialsFile: ptr.To("creds-file-1"),
									},
									BasicAuth: &v1.AlertManagerClientBasicAuth{
										Username: ptr.To("user-1"),
										Password: ptr.To("pass-1"),
									},
								},
							},
						},
						"tenant-2": {
							AlertManagerOverrides: &v1.AlertManagerSpec{
								ExternalURL: "external-2.io",
								ExternalLabels: map[string]string{
									"dink-2": "doink-2",
									"yin-2":  "yang-2",
								},
								EnableV2:  false,
								Endpoints: []string{"t-2-ep-1", "t-2-ep-2"},
								DiscoverySpec: &v1.AlertManagerDiscoverySpec{
									EnableSRV:       false,
									RefreshInterval: "43s",
								},
								NotificationQueueSpec: &v1.AlertManagerNotificationQueueSpec{
									Capacity:           300,
									Timeout:            v1.PrometheusDuration("4m"),
									ForOutageTolerance: v1.PrometheusDuration("11s"),
									ForGracePeriod:     v1.PrometheusDuration("9m"),
									ResendDelay:        "3s",
								},
								RelabelConfigs: []v1.RelabelConfig{
									{
										SourceLabels: []string{"r2l1-2", "r2l2-2"},
										Separator:    ".",
										TargetLabel:  "new-relabel-21",
										Regex:        "^[1-8]",
										Modulus:      70,
										Replacement:  "$3",
										Action:       "labelkeep",
									},
									{
										SourceLabels: []string{"r2l21-2", "r2l22-2"},
										Separator:    ":",
										TargetLabel:  "new-relabel-22",
										Regex:        "[a-zA-Z0-9_]",
										Modulus:      87,
										Replacement:  "$5",
										Action:       "replace",
									},
								},
								Client: &v1.AlertManagerClientConfig{
									TLS: &v1.AlertManagerClientTLSConfig{
										CAPath:     ptr.To("/tls/ca/path-1"),
										ServerName: ptr.To("server-1"),
										CertPath:   ptr.To("/tls/cert/path-1"),
										KeyPath:    ptr.To("/tls/key/path-1"),
									},
									HeaderAuth: &v1.AlertManagerClientHeaderAuth{
										Type:            ptr.To("type-1"),
										Credentials:     ptr.To("creds-1"),
										CredentialsFile: ptr.To("creds-file-1"),
									},
									BasicAuth: &v1.AlertManagerClientBasicAuth{
										Username: ptr.To("user-1"),
										Password: ptr.To("pass-1"),
									},
								},
							},
						},
					},
				},
				Status: v1.RulerConfigStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(v1.ConditionReady),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(v1.ConditionPending),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
			want: v1beta1.RulerConfig{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lokistack-dev",
					Namespace: "jupiter",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1beta1.RulerConfigSpec{
					EvalutionInterval: "1m",
					PollInterval:      "2m",
					AlertManagerSpec: &v1beta1.AlertManagerSpec{
						ExternalURL: "am.io",
						ExternalLabels: map[string]string{
							"foo": "bar",
							"tik": "tok",
						},
						EnableV2:  true,
						Endpoints: []string{"endpoint-1", "endpoint-2"},
						DiscoverySpec: &v1beta1.AlertManagerDiscoverySpec{
							EnableSRV:       true,
							RefreshInterval: "3m",
						},
						NotificationQueueSpec: &v1beta1.AlertManagerNotificationQueueSpec{
							Capacity:           500,
							Timeout:            v1beta1.PrometheusDuration("5s"),
							ForOutageTolerance: v1beta1.PrometheusDuration("10m"),
							ForGracePeriod:     v1beta1.PrometheusDuration("10s"),
							ResendDelay:        "1s",
						},
						RelabelConfigs: []v1beta1.RelabelConfig{
							{
								SourceLabels: []string{"l1", "l2"},
								Separator:    "|",
								TargetLabel:  "new-label",
								Regex:        "*.",
								Modulus:      4,
								Replacement:  "$2",
								Action:       "replace",
							},
							{
								SourceLabels: []string{"l21", "l22"},
								Separator:    ",",
								TargetLabel:  "newer-label",
								Regex:        "^[a-zA-Z-]",
								Modulus:      42,
								Replacement:  "$3",
								Action:       "drop",
							},
						},
						Client: &v1beta1.AlertManagerClientConfig{
							TLS: &v1beta1.AlertManagerClientTLSConfig{
								CAPath:     ptr.To("/tls/ca/path"),
								ServerName: ptr.To("server"),
								CertPath:   ptr.To("/tls/cert/path"),
								KeyPath:    ptr.To("/tls/key/path"),
							},
							HeaderAuth: &v1beta1.AlertManagerClientHeaderAuth{
								Type:            ptr.To("type"),
								Credentials:     ptr.To("creds"),
								CredentialsFile: ptr.To("creds-file"),
							},
							BasicAuth: &v1beta1.AlertManagerClientBasicAuth{
								Username: ptr.To("user"),
								Password: ptr.To("pass"),
							},
						},
					},
					RemoteWriteSpec: &v1beta1.RemoteWriteSpec{
						Enabled:       true,
						RefreshPeriod: "7m",
						ClientSpec: &v1beta1.RemoteWriteClientSpec{
							Name:                    "remote",
							URL:                     "remote.fr",
							Timeout:                 "8s",
							AuthorizationType:       v1beta1.BearerAuthorization,
							AuthorizationSecretName: "secret",
							AdditionalHeaders: map[string]string{
								"flip": "flop",
								"tic":  "tac",
							},
							RelabelConfigs: []v1beta1.RelabelConfig{
								{
									SourceLabels: []string{"rl1", "rl2"},
									Separator:    ";",
									TargetLabel:  "new-relabel",
									Regex:        "^[1-9]",
									Modulus:      711,
									Replacement:  "$6",
									Action:       "keep",
								},
								{
									SourceLabels: []string{"rl21", "rl22"},
									Separator:    "/",
									TargetLabel:  "newer-relabel",
									Regex:        "^[A-Z-]",
									Modulus:      117,
									Replacement:  "$3",
									Action:       "hashmod",
								},
							},
							ProxyURL:        "proxy",
							FollowRedirects: true,
						},
					},
					Overrides: map[string]v1beta1.RulerOverrides{
						"tenant-1": {
							AlertManagerOverrides: &v1beta1.AlertManagerSpec{
								ExternalURL: "external.io",
								ExternalLabels: map[string]string{
									"dink": "doink",
									"yin":  "yang",
								},
								EnableV2:  true,
								Endpoints: []string{"t-ep-1", "t-ep-2"},
								DiscoverySpec: &v1beta1.AlertManagerDiscoverySpec{
									EnableSRV:       true,
									RefreshInterval: "42s",
								},
								NotificationQueueSpec: &v1beta1.AlertManagerNotificationQueueSpec{
									Capacity:           350,
									Timeout:            v1beta1.PrometheusDuration("5m"),
									ForOutageTolerance: v1beta1.PrometheusDuration("10s"),
									ForGracePeriod:     v1beta1.PrometheusDuration("10m"),
									ResendDelay:        "2s",
								},
								RelabelConfigs: []v1beta1.RelabelConfig{
									{
										SourceLabels: []string{"r2l1", "r2l2"},
										Separator:    "-",
										TargetLabel:  "new-relabel-1",
										Regex:        "^[0-7]",
										Modulus:      71,
										Replacement:  "$9",
										Action:       "labeldrop",
									},
									{
										SourceLabels: []string{"r2l21", "r2l22"},
										Separator:    "%",
										TargetLabel:  "new-relabel-2",
										Regex:        "^[a-z]",
										Modulus:      56,
										Replacement:  "$4",
										Action:       "labelkeep",
									},
								},
								Client: &v1beta1.AlertManagerClientConfig{
									TLS: &v1beta1.AlertManagerClientTLSConfig{
										CAPath:     ptr.To("/tls/ca/path-1"),
										ServerName: ptr.To("server-1"),
										CertPath:   ptr.To("/tls/cert/path-1"),
										KeyPath:    ptr.To("/tls/key/path-1"),
									},
									HeaderAuth: &v1beta1.AlertManagerClientHeaderAuth{
										Type:            ptr.To("type-1"),
										Credentials:     ptr.To("creds-1"),
										CredentialsFile: ptr.To("creds-file-1"),
									},
									BasicAuth: &v1beta1.AlertManagerClientBasicAuth{
										Username: ptr.To("user-1"),
										Password: ptr.To("pass-1"),
									},
								},
							},
						},
						"tenant-2": {
							AlertManagerOverrides: &v1beta1.AlertManagerSpec{
								ExternalURL: "external-2.io",
								ExternalLabels: map[string]string{
									"dink-2": "doink-2",
									"yin-2":  "yang-2",
								},
								EnableV2:  false,
								Endpoints: []string{"t-2-ep-1", "t-2-ep-2"},
								DiscoverySpec: &v1beta1.AlertManagerDiscoverySpec{
									EnableSRV:       false,
									RefreshInterval: "43s",
								},
								NotificationQueueSpec: &v1beta1.AlertManagerNotificationQueueSpec{
									Capacity:           300,
									Timeout:            v1beta1.PrometheusDuration("4m"),
									ForOutageTolerance: v1beta1.PrometheusDuration("11s"),
									ForGracePeriod:     v1beta1.PrometheusDuration("9m"),
									ResendDelay:        "3s",
								},
								RelabelConfigs: []v1beta1.RelabelConfig{
									{
										SourceLabels: []string{"r2l1-2", "r2l2-2"},
										Separator:    ".",
										TargetLabel:  "new-relabel-21",
										Regex:        "^[1-8]",
										Modulus:      70,
										Replacement:  "$3",
										Action:       "labelkeep",
									},
									{
										SourceLabels: []string{"r2l21-2", "r2l22-2"},
										Separator:    ":",
										TargetLabel:  "new-relabel-22",
										Regex:        "[a-zA-Z0-9_]",
										Modulus:      87,
										Replacement:  "$5",
										Action:       "replace",
									},
								},
								Client: &v1beta1.AlertManagerClientConfig{
									TLS: &v1beta1.AlertManagerClientTLSConfig{
										CAPath:     ptr.To("/tls/ca/path-1"),
										ServerName: ptr.To("server-1"),
										CertPath:   ptr.To("/tls/cert/path-1"),
										KeyPath:    ptr.To("/tls/key/path-1"),
									},
									HeaderAuth: &v1beta1.AlertManagerClientHeaderAuth{
										Type:            ptr.To("type-1"),
										Credentials:     ptr.To("creds-1"),
										CredentialsFile: ptr.To("creds-file-1"),
									},
									BasicAuth: &v1beta1.AlertManagerClientBasicAuth{
										Username: ptr.To("user-1"),
										Password: ptr.To("pass-1"),
									},
								},
							},
						},
					},
				},
				Status: v1beta1.RulerConfigStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(v1.ConditionReady),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(v1.ConditionPending),
							Status: metav1.ConditionFalse,
						},
					},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			dst := v1beta1.RulerConfig{}
			err := dst.ConvertFrom(&tc.src)
			require.NoError(t, err)
			require.Equal(t, dst, tc.want)
		})
	}
}
