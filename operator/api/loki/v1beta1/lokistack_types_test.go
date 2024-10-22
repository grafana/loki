package v1beta1_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	v1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/api/loki/v1beta1"
)

func TestConvertToV1_LokiStack(t *testing.T) {
	tt := []struct {
		desc string
		src  v1beta1.LokiStack
		want v1.LokiStack
	}{
		{
			desc: "empty src(v1beta1) and dst(v1) lokistack",
			src:  v1beta1.LokiStack{},
			want: v1.LokiStack{},
		},
		{
			desc: "full conversion of src(v1beta1) to dst(v1) lokistack",
			src: v1beta1.LokiStack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lokistack-dev",
					Namespace: "mercury",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1beta1.LokiStackSpec{
					ManagementState: v1beta1.ManagementStateManaged,
					Size:            v1beta1.SizeOneXMedium,
					Storage: v1beta1.ObjectStorageSpec{
						Schemas: []v1beta1.ObjectStorageSchema{
							{
								EffectiveDate: v1beta1.StorageSchemaEffectiveDate("2020-11-20"),
								Version:       v1beta1.ObjectStorageSchemaV11,
							},
							{
								EffectiveDate: v1beta1.StorageSchemaEffectiveDate("2021-11-20"),
								Version:       v1beta1.ObjectStorageSchemaV12,
							},
						},
						Secret: v1beta1.ObjectStorageSecretSpec{
							Type: v1beta1.ObjectStorageSecretS3,
							Name: "test",
						},
						TLS: &v1beta1.ObjectStorageTLSSpec{
							CA: "test-ca",
						},
					},
					StorageClassName:  "standard",
					ReplicationFactor: 2,
					Rules: &v1beta1.RulesSpec{
						Enabled: true,
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"key": "Value",
							},
						},
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"key": "Value",
							},
						},
					},
					Limits: &v1beta1.LimitsSpec{
						Global: &v1beta1.LimitsTemplateSpec{
							IngestionLimits: &v1beta1.IngestionLimitSpec{
								IngestionRate:             100,
								IngestionBurstSize:        200,
								MaxLabelNameLength:        1000,
								MaxLabelValueLength:       1000,
								MaxLabelNamesPerSeries:    1000,
								MaxGlobalStreamsPerTenant: 10000,
								MaxLineSize:               512,
							},
							QueryLimits: &v1beta1.QueryLimitSpec{
								MaxEntriesLimitPerQuery: 1000,
								MaxChunksPerQuery:       1000,
								MaxQuerySeries:          10000,
							},
						},
						Tenants: map[string]v1beta1.LimitsTemplateSpec{
							"tenant-a": {
								IngestionLimits: &v1beta1.IngestionLimitSpec{
									IngestionRate:             100,
									IngestionBurstSize:        200,
									MaxLabelNameLength:        1000,
									MaxLabelValueLength:       1000,
									MaxLabelNamesPerSeries:    1000,
									MaxGlobalStreamsPerTenant: 10000,
									MaxLineSize:               512,
								},
								QueryLimits: &v1beta1.QueryLimitSpec{
									MaxEntriesLimitPerQuery: 1000,
									MaxChunksPerQuery:       1000,
									MaxQuerySeries:          10000,
								},
							},
							"tenant-b": {
								IngestionLimits: &v1beta1.IngestionLimitSpec{
									IngestionRate:             100,
									IngestionBurstSize:        200,
									MaxLabelNameLength:        1000,
									MaxLabelValueLength:       1000,
									MaxLabelNamesPerSeries:    1000,
									MaxGlobalStreamsPerTenant: 10000,
									MaxLineSize:               512,
								},
								QueryLimits: &v1beta1.QueryLimitSpec{
									MaxEntriesLimitPerQuery: 1000,
									MaxChunksPerQuery:       1000,
									MaxQuerySeries:          10000,
								},
							},
						},
					},
					Template: &v1beta1.LokiTemplateSpec{
						Compactor: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Distributor: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Ingester: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Querier: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						QueryFrontend: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Gateway: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						IndexGateway: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Ruler: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
					},
					Tenants: &v1beta1.TenantsSpec{
						Mode: v1beta1.Dynamic,
						Authentication: []v1beta1.AuthenticationSpec{
							{
								TenantName: "tenant-a",
								TenantID:   "tenant-a",
								OIDC: &v1beta1.OIDCSpec{
									Secret: &v1beta1.TenantSecretSpec{
										Name: "tenant-a-secret",
									},
									IssuerURL:     "http://go-to-issuer",
									RedirectURL:   "http://bring-me-back",
									GroupClaim:    "workgroups",
									UsernameClaim: "email",
								},
							},
							{
								TenantName: "tenant-b",
								TenantID:   "tenant-b",
								OIDC: &v1beta1.OIDCSpec{
									Secret: &v1beta1.TenantSecretSpec{
										Name: "tenant-a-secret",
									},
									IssuerURL:     "http://go-to-issuer",
									RedirectURL:   "http://bring-me-back",
									GroupClaim:    "workgroups",
									UsernameClaim: "email",
								},
							},
						},
						Authorization: &v1beta1.AuthorizationSpec{
							OPA: &v1beta1.OPASpec{
								URL: "http://authorize-me/opa",
							},
							Roles: []v1beta1.RoleSpec{
								{
									Name:        "ro-role",
									Resources:   []string{"logs"},
									Tenants:     []string{"tenant-a", "tenant-b"},
									Permissions: []v1beta1.PermissionType{v1beta1.Read},
								},
								{
									Name:        "rw-role",
									Resources:   []string{"logs"},
									Tenants:     []string{"tenant-a", "tenant-b"},
									Permissions: []v1beta1.PermissionType{v1beta1.Read, v1beta1.Write},
								},
							},
							RoleBindings: []v1beta1.RoleBindingsSpec{
								{
									Name:  "bind-me",
									Roles: []string{"ro-role"},
									Subjects: []v1beta1.Subject{
										{
											Name: "a-user",
											Kind: v1beta1.User,
										},
										{
											Name: "a-group",
											Kind: v1beta1.Group,
										},
									},
								},
							},
						},
					},
				},
				Status: v1beta1.LokiStackStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(v1beta1.ConditionReady),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(v1beta1.ConditionPending),
							Status: metav1.ConditionFalse,
						},
					},
					Components: v1beta1.LokiStackComponentStatus{
						Compactor: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Distributor: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Ingester: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Querier: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						QueryFrontend: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						IndexGateway: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Ruler: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Gateway: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
					},
					Storage: v1beta1.LokiStackStorageStatus{
						Schemas: []v1beta1.ObjectStorageSchema{
							{
								Version:       v1beta1.ObjectStorageSchemaV11,
								EffectiveDate: "2020-06-01",
							},
						},
					},
				},
			},
			want: v1.LokiStack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lokistack-dev",
					Namespace: "mercury",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1.LokiStackSpec{
					ManagementState: v1.ManagementStateManaged,
					Size:            v1.SizeOneXMedium,
					Storage: v1.ObjectStorageSpec{
						Schemas: []v1.ObjectStorageSchema{
							{
								EffectiveDate: v1.StorageSchemaEffectiveDate("2020-11-20"),
								Version:       v1.ObjectStorageSchemaV11,
							},
							{
								EffectiveDate: v1.StorageSchemaEffectiveDate("2021-11-20"),
								Version:       v1.ObjectStorageSchemaV12,
							},
						},
						Secret: v1.ObjectStorageSecretSpec{
							Type: v1.ObjectStorageSecretS3,
							Name: "test",
						},
						TLS: &v1.ObjectStorageTLSSpec{
							CASpec: v1.CASpec{
								CA: "test-ca",
							},
						},
					},
					StorageClassName:  "standard",
					ReplicationFactor: 2,
					Rules: &v1.RulesSpec{
						Enabled: true,
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"key": "Value",
							},
						},
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"key": "Value",
							},
						},
					},
					Limits: &v1.LimitsSpec{
						Global: &v1.LimitsTemplateSpec{
							IngestionLimits: &v1.IngestionLimitSpec{
								IngestionRate:             100,
								IngestionBurstSize:        200,
								MaxLabelNameLength:        1000,
								MaxLabelValueLength:       1000,
								MaxLabelNamesPerSeries:    1000,
								MaxGlobalStreamsPerTenant: 10000,
								MaxLineSize:               512,
							},
							QueryLimits: &v1.QueryLimitSpec{
								MaxEntriesLimitPerQuery: 1000,
								MaxChunksPerQuery:       1000,
								MaxQuerySeries:          10000,
							},
						},
						Tenants: map[string]v1.PerTenantLimitsTemplateSpec{
							"tenant-a": {
								IngestionLimits: &v1.IngestionLimitSpec{
									IngestionRate:             100,
									IngestionBurstSize:        200,
									MaxLabelNameLength:        1000,
									MaxLabelValueLength:       1000,
									MaxLabelNamesPerSeries:    1000,
									MaxGlobalStreamsPerTenant: 10000,
									MaxLineSize:               512,
								},
								QueryLimits: &v1.PerTenantQueryLimitSpec{
									QueryLimitSpec: v1.QueryLimitSpec{
										MaxEntriesLimitPerQuery: 1000,
										MaxChunksPerQuery:       1000,
										MaxQuerySeries:          10000,
									},
								},
							},
							"tenant-b": {
								IngestionLimits: &v1.IngestionLimitSpec{
									IngestionRate:             100,
									IngestionBurstSize:        200,
									MaxLabelNameLength:        1000,
									MaxLabelValueLength:       1000,
									MaxLabelNamesPerSeries:    1000,
									MaxGlobalStreamsPerTenant: 10000,
									MaxLineSize:               512,
								},
								QueryLimits: &v1.PerTenantQueryLimitSpec{
									QueryLimitSpec: v1.QueryLimitSpec{
										MaxEntriesLimitPerQuery: 1000,
										MaxChunksPerQuery:       1000,
										MaxQuerySeries:          10000,
									},
								},
							},
						},
					},
					Template: &v1.LokiTemplateSpec{
						Compactor: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Distributor: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Ingester: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Querier: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						QueryFrontend: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Gateway: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						IndexGateway: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Ruler: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
					},
					Tenants: &v1.TenantsSpec{
						Mode: v1.Dynamic,
						Authentication: []v1.AuthenticationSpec{
							{
								TenantName: "tenant-a",
								TenantID:   "tenant-a",
								OIDC: &v1.OIDCSpec{
									Secret: &v1.TenantSecretSpec{
										Name: "tenant-a-secret",
									},
									IssuerURL:     "http://go-to-issuer",
									RedirectURL:   "http://bring-me-back",
									GroupClaim:    "workgroups",
									UsernameClaim: "email",
								},
							},
							{
								TenantName: "tenant-b",
								TenantID:   "tenant-b",
								OIDC: &v1.OIDCSpec{
									Secret: &v1.TenantSecretSpec{
										Name: "tenant-a-secret",
									},
									IssuerURL:     "http://go-to-issuer",
									RedirectURL:   "http://bring-me-back",
									GroupClaim:    "workgroups",
									UsernameClaim: "email",
								},
							},
						},
						Authorization: &v1.AuthorizationSpec{
							OPA: &v1.OPASpec{
								URL: "http://authorize-me/opa",
							},
							Roles: []v1.RoleSpec{
								{
									Name:        "ro-role",
									Resources:   []string{"logs"},
									Tenants:     []string{"tenant-a", "tenant-b"},
									Permissions: []v1.PermissionType{v1.Read},
								},
								{
									Name:        "rw-role",
									Resources:   []string{"logs"},
									Tenants:     []string{"tenant-a", "tenant-b"},
									Permissions: []v1.PermissionType{v1.Read, v1.Write},
								},
							},
							RoleBindings: []v1.RoleBindingsSpec{
								{
									Name:  "bind-me",
									Roles: []string{"ro-role"},
									Subjects: []v1.Subject{
										{
											Name: "a-user",
											Kind: v1.User,
										},
										{
											Name: "a-group",
											Kind: v1.Group,
										},
									},
								},
							},
						},
					},
				},
				Status: v1.LokiStackStatus{
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
					Components: v1.LokiStackComponentStatus{
						Compactor: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Distributor: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Ingester: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Querier: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						QueryFrontend: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						IndexGateway: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Ruler: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Gateway: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
					},
					Storage: v1.LokiStackStorageStatus{
						Schemas: []v1.ObjectStorageSchema{
							{
								Version:       v1.ObjectStorageSchemaV11,
								EffectiveDate: "2020-06-01",
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			dst := v1.LokiStack{}
			err := tc.src.ConvertTo(&dst)
			require.NoError(t, err)
			require.Equal(t, dst, tc.want)
		})
	}
}

func TestConvertFromV1_LokiStack(t *testing.T) {
	tt := []struct {
		desc string
		src  v1.LokiStack
		want v1beta1.LokiStack
	}{
		{
			desc: "empty src(v1) and dst(v1beta1) lokistack",
			src:  v1.LokiStack{},
			want: v1beta1.LokiStack{},
		},
		{
			desc: "full conversion of src(v1) to dst(v1beta1) lokistack",
			src: v1.LokiStack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lokistack-dev",
					Namespace: "mercury",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1.LokiStackSpec{
					ManagementState: v1.ManagementStateManaged,
					Size:            v1.SizeOneXMedium,
					Storage: v1.ObjectStorageSpec{
						Schemas: []v1.ObjectStorageSchema{
							{
								EffectiveDate: v1.StorageSchemaEffectiveDate("2020-11-20"),
								Version:       v1.ObjectStorageSchemaV11,
							},
							{
								EffectiveDate: v1.StorageSchemaEffectiveDate("2021-11-20"),
								Version:       v1.ObjectStorageSchemaV12,
							},
						},
						Secret: v1.ObjectStorageSecretSpec{
							Type: v1.ObjectStorageSecretS3,
							Name: "test",
						},
						TLS: &v1.ObjectStorageTLSSpec{
							CASpec: v1.CASpec{
								CA: "test-ca",
							},
						},
					},
					StorageClassName:  "standard",
					ReplicationFactor: 2,
					Rules: &v1.RulesSpec{
						Enabled: true,
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"key": "Value",
							},
						},
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"key": "Value",
							},
						},
					},
					Limits: &v1.LimitsSpec{
						Global: &v1.LimitsTemplateSpec{
							IngestionLimits: &v1.IngestionLimitSpec{
								IngestionRate:             100,
								IngestionBurstSize:        200,
								MaxLabelNameLength:        1000,
								MaxLabelValueLength:       1000,
								MaxLabelNamesPerSeries:    1000,
								MaxGlobalStreamsPerTenant: 10000,
								MaxLineSize:               512,
							},
							QueryLimits: &v1.QueryLimitSpec{
								MaxEntriesLimitPerQuery: 1000,
								MaxChunksPerQuery:       1000,
								MaxQuerySeries:          10000,
							},
						},
						Tenants: map[string]v1.PerTenantLimitsTemplateSpec{
							"tenant-a": {
								IngestionLimits: &v1.IngestionLimitSpec{
									IngestionRate:             100,
									IngestionBurstSize:        200,
									MaxLabelNameLength:        1000,
									MaxLabelValueLength:       1000,
									MaxLabelNamesPerSeries:    1000,
									MaxGlobalStreamsPerTenant: 10000,
									MaxLineSize:               512,
								},
								QueryLimits: &v1.PerTenantQueryLimitSpec{
									QueryLimitSpec: v1.QueryLimitSpec{
										MaxEntriesLimitPerQuery: 1000,
										MaxChunksPerQuery:       1000,
										MaxQuerySeries:          10000,
									},
								},
							},
							"tenant-b": {
								IngestionLimits: &v1.IngestionLimitSpec{
									IngestionRate:             100,
									IngestionBurstSize:        200,
									MaxLabelNameLength:        1000,
									MaxLabelValueLength:       1000,
									MaxLabelNamesPerSeries:    1000,
									MaxGlobalStreamsPerTenant: 10000,
									MaxLineSize:               512,
								},
								QueryLimits: &v1.PerTenantQueryLimitSpec{
									QueryLimitSpec: v1.QueryLimitSpec{
										MaxEntriesLimitPerQuery: 1000,
										MaxChunksPerQuery:       1000,
										MaxQuerySeries:          10000,
									},
								},
							},
						},
					},
					Template: &v1.LokiTemplateSpec{
						Compactor: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Distributor: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Ingester: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Querier: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						QueryFrontend: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Gateway: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						IndexGateway: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Ruler: &v1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
					},
					Tenants: &v1.TenantsSpec{
						Mode: v1.Dynamic,
						Authentication: []v1.AuthenticationSpec{
							{
								TenantName: "tenant-a",
								TenantID:   "tenant-a",
								OIDC: &v1.OIDCSpec{
									Secret: &v1.TenantSecretSpec{
										Name: "tenant-a-secret",
									},
									IssuerURL:     "http://go-to-issuer",
									RedirectURL:   "http://bring-me-back",
									GroupClaim:    "workgroups",
									UsernameClaim: "email",
								},
							},
							{
								TenantName: "tenant-b",
								TenantID:   "tenant-b",
								OIDC: &v1.OIDCSpec{
									Secret: &v1.TenantSecretSpec{
										Name: "tenant-a-secret",
									},
									IssuerURL:     "http://go-to-issuer",
									RedirectURL:   "http://bring-me-back",
									GroupClaim:    "workgroups",
									UsernameClaim: "email",
								},
							},
						},
						Authorization: &v1.AuthorizationSpec{
							OPA: &v1.OPASpec{
								URL: "http://authorize-me/opa",
							},
							Roles: []v1.RoleSpec{
								{
									Name:        "ro-role",
									Resources:   []string{"logs"},
									Tenants:     []string{"tenant-a", "tenant-b"},
									Permissions: []v1.PermissionType{v1.Read},
								},
								{
									Name:        "rw-role",
									Resources:   []string{"logs"},
									Tenants:     []string{"tenant-a", "tenant-b"},
									Permissions: []v1.PermissionType{v1.Read, v1.Write},
								},
							},
							RoleBindings: []v1.RoleBindingsSpec{
								{
									Name:  "bind-me",
									Roles: []string{"ro-role"},
									Subjects: []v1.Subject{
										{
											Name: "a-user",
											Kind: v1.User,
										},
										{
											Name: "a-group",
											Kind: v1.Group,
										},
									},
								},
							},
						},
					},
				},
				Status: v1.LokiStackStatus{
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
					Components: v1.LokiStackComponentStatus{
						Compactor: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Distributor: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Ingester: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Querier: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						QueryFrontend: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						IndexGateway: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Ruler: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Gateway: v1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
					},
					Storage: v1.LokiStackStorageStatus{
						Schemas: []v1.ObjectStorageSchema{
							{
								Version:       v1.ObjectStorageSchemaV11,
								EffectiveDate: "2020-06-01",
							},
						},
					},
				},
			},
			want: v1beta1.LokiStack{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "lokistack-dev",
					Namespace: "mercury",
					Labels: map[string]string{
						"app":     "loki",
						"part-of": "lokistack",
					},
					Annotations: map[string]string{
						"discoveredAt": "2022-06-28",
					},
				},
				Spec: v1beta1.LokiStackSpec{
					ManagementState: v1beta1.ManagementStateManaged,
					Size:            v1beta1.SizeOneXMedium,
					Storage: v1beta1.ObjectStorageSpec{
						Schemas: []v1beta1.ObjectStorageSchema{
							{
								EffectiveDate: v1beta1.StorageSchemaEffectiveDate("2020-11-20"),
								Version:       v1beta1.ObjectStorageSchemaV11,
							},
							{
								EffectiveDate: v1beta1.StorageSchemaEffectiveDate("2021-11-20"),
								Version:       v1beta1.ObjectStorageSchemaV12,
							},
						},
						Secret: v1beta1.ObjectStorageSecretSpec{
							Type: v1beta1.ObjectStorageSecretS3,
							Name: "test",
						},
						TLS: &v1beta1.ObjectStorageTLSSpec{
							CA: "test-ca",
						},
					},
					StorageClassName:  "standard",
					ReplicationFactor: 2,
					Rules: &v1beta1.RulesSpec{
						Enabled: true,
						Selector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"key": "Value",
							},
						},
						NamespaceSelector: &metav1.LabelSelector{
							MatchLabels: map[string]string{
								"key": "Value",
							},
						},
					},
					Limits: &v1beta1.LimitsSpec{
						Global: &v1beta1.LimitsTemplateSpec{
							IngestionLimits: &v1beta1.IngestionLimitSpec{
								IngestionRate:             100,
								IngestionBurstSize:        200,
								MaxLabelNameLength:        1000,
								MaxLabelValueLength:       1000,
								MaxLabelNamesPerSeries:    1000,
								MaxGlobalStreamsPerTenant: 10000,
								MaxLineSize:               512,
							},
							QueryLimits: &v1beta1.QueryLimitSpec{
								MaxEntriesLimitPerQuery: 1000,
								MaxChunksPerQuery:       1000,
								MaxQuerySeries:          10000,
							},
						},
						Tenants: map[string]v1beta1.LimitsTemplateSpec{
							"tenant-a": {
								IngestionLimits: &v1beta1.IngestionLimitSpec{
									IngestionRate:             100,
									IngestionBurstSize:        200,
									MaxLabelNameLength:        1000,
									MaxLabelValueLength:       1000,
									MaxLabelNamesPerSeries:    1000,
									MaxGlobalStreamsPerTenant: 10000,
									MaxLineSize:               512,
								},
								QueryLimits: &v1beta1.QueryLimitSpec{
									MaxEntriesLimitPerQuery: 1000,
									MaxChunksPerQuery:       1000,
									MaxQuerySeries:          10000,
								},
							},
							"tenant-b": {
								IngestionLimits: &v1beta1.IngestionLimitSpec{
									IngestionRate:             100,
									IngestionBurstSize:        200,
									MaxLabelNameLength:        1000,
									MaxLabelValueLength:       1000,
									MaxLabelNamesPerSeries:    1000,
									MaxGlobalStreamsPerTenant: 10000,
									MaxLineSize:               512,
								},
								QueryLimits: &v1beta1.QueryLimitSpec{
									MaxEntriesLimitPerQuery: 1000,
									MaxChunksPerQuery:       1000,
									MaxQuerySeries:          10000,
								},
							},
						},
					},
					Template: &v1beta1.LokiTemplateSpec{
						Compactor: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Distributor: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Ingester: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Querier: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						QueryFrontend: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Gateway: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						IndexGateway: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
						Ruler: &v1beta1.LokiComponentSpec{
							Replicas:     1,
							NodeSelector: map[string]string{"node": "a"},
							Tolerations: []corev1.Toleration{
								{
									Key:   "tolerate",
									Value: "this",
								},
							},
						},
					},
					Tenants: &v1beta1.TenantsSpec{
						Mode: v1beta1.Dynamic,
						Authentication: []v1beta1.AuthenticationSpec{
							{
								TenantName: "tenant-a",
								TenantID:   "tenant-a",
								OIDC: &v1beta1.OIDCSpec{
									Secret: &v1beta1.TenantSecretSpec{
										Name: "tenant-a-secret",
									},
									IssuerURL:     "http://go-to-issuer",
									RedirectURL:   "http://bring-me-back",
									GroupClaim:    "workgroups",
									UsernameClaim: "email",
								},
							},
							{
								TenantName: "tenant-b",
								TenantID:   "tenant-b",
								OIDC: &v1beta1.OIDCSpec{
									Secret: &v1beta1.TenantSecretSpec{
										Name: "tenant-a-secret",
									},
									IssuerURL:     "http://go-to-issuer",
									RedirectURL:   "http://bring-me-back",
									GroupClaim:    "workgroups",
									UsernameClaim: "email",
								},
							},
						},
						Authorization: &v1beta1.AuthorizationSpec{
							OPA: &v1beta1.OPASpec{
								URL: "http://authorize-me/opa",
							},
							Roles: []v1beta1.RoleSpec{
								{
									Name:        "ro-role",
									Resources:   []string{"logs"},
									Tenants:     []string{"tenant-a", "tenant-b"},
									Permissions: []v1beta1.PermissionType{v1beta1.Read},
								},
								{
									Name:        "rw-role",
									Resources:   []string{"logs"},
									Tenants:     []string{"tenant-a", "tenant-b"},
									Permissions: []v1beta1.PermissionType{v1beta1.Read, v1beta1.Write},
								},
							},
							RoleBindings: []v1beta1.RoleBindingsSpec{
								{
									Name:  "bind-me",
									Roles: []string{"ro-role"},
									Subjects: []v1beta1.Subject{
										{
											Name: "a-user",
											Kind: v1beta1.User,
										},
										{
											Name: "a-group",
											Kind: v1beta1.Group,
										},
									},
								},
							},
						},
					},
				},
				Status: v1beta1.LokiStackStatus{
					Conditions: []metav1.Condition{
						{
							Type:   string(v1beta1.ConditionReady),
							Status: metav1.ConditionTrue,
						},
						{
							Type:   string(v1beta1.ConditionPending),
							Status: metav1.ConditionFalse,
						},
					},
					Components: v1beta1.LokiStackComponentStatus{
						Compactor: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Distributor: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Ingester: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Querier: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						QueryFrontend: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						IndexGateway: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Ruler: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
						Gateway: v1beta1.PodStatusMap{
							"ready": []string{"pod-1"},
						},
					},
					Storage: v1beta1.LokiStackStorageStatus{
						Schemas: []v1beta1.ObjectStorageSchema{
							{
								Version:       v1beta1.ObjectStorageSchemaV11,
								EffectiveDate: "2020-06-01",
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			dst := v1beta1.LokiStack{}
			err := dst.ConvertFrom(&tc.src)
			require.NoError(t, err)
			require.Equal(t, dst, tc.want)
		})
	}
}
