package metrics

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/ViaQ/logerr/v2/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

func TestRegisterLokiStackMetrics(t *testing.T) {
	logger := log.NewLogger("test", log.WithOutput(io.Discard))
	client := &k8sfakes.FakeClient{}
	registry := prometheus.NewPedanticRegistry()

	err := RegisterLokiStackCollector(logger, client, registry)
	require.NoError(t, err)
}

func TestLokiStackMetricsCollect(t *testing.T) {
	tt := []struct {
		desc        string
		k8sError    error
		stacks      *lokiv1.LokiStackList
		wantMetrics string
	}{
		{
			desc:        "no stacks",
			k8sError:    nil,
			stacks:      &lokiv1.LokiStackList{},
			wantMetrics: "",
		},
		{
			desc:     "one demo",
			k8sError: nil,
			stacks: &lokiv1.LokiStackList{
				Items: []lokiv1.LokiStack{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-stack",
							Namespace: "test-namespace",
						},
						Spec: lokiv1.LokiStackSpec{
							Size: lokiv1.SizeOneXDemo,
							Storage: lokiv1.ObjectStorageSpec{
								Secret: lokiv1.ObjectStorageSecretSpec{
									Type: lokiv1.ObjectStorageSecretS3,
									Name: "storage-secret",
								},
							},
						},
					},
				},
			},
			wantMetrics: `# HELP lokistack_info Information about deployed LokiStack instances. Value is always 1.
# TYPE lokistack_info gauge
lokistack_info{size="1x.demo",stack_name="test-stack",stack_namespace="test-namespace"} 1
# HELP lokistack_status_condition Counts the current status conditions of the LokiStack.
# TYPE lokistack_status_condition gauge
lokistack_status_condition{condition="Degraded",reason="",size="1x.demo",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Degraded",reason="",size="1x.demo",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
lokistack_status_condition{condition="Failed",reason="",size="1x.demo",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Failed",reason="",size="1x.demo",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
lokistack_status_condition{condition="Pending",reason="",size="1x.demo",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Pending",reason="",size="1x.demo",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
lokistack_status_condition{condition="Ready",reason="",size="1x.demo",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Ready",reason="",size="1x.demo",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
# HELP lokistack_storage_info Information about LokiStack storage backend configuration.
# TYPE lokistack_storage_info gauge
lokistack_storage_info{backend_type="s3",credential_mode="static",size="1x.demo",stack_name="test-stack",stack_namespace="test-namespace"} 1
`,
		},
		{
			desc:     "one small with warning",
			k8sError: nil,
			stacks: &lokiv1.LokiStackList{
				Items: []lokiv1.LokiStack{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-stack",
							Namespace: "test-namespace",
						},
						Spec: lokiv1.LokiStackSpec{
							Size: lokiv1.SizeOneXSmall,
							Storage: lokiv1.ObjectStorageSpec{
								Secret: lokiv1.ObjectStorageSecretSpec{
									Type: lokiv1.ObjectStorageSecretS3,
									Name: "storage-secret",
								},
							},
						},
						Status: lokiv1.LokiStackStatus{
							Conditions: []metav1.Condition{
								{
									Type:   string(lokiv1.ConditionWarning),
									Status: metav1.ConditionTrue,
									Reason: string(lokiv1.ReasonStorageNeedsSchemaUpdate),
								},
							},
						},
					},
				},
			},
			wantMetrics: `# HELP lokistack_info Information about deployed LokiStack instances. Value is always 1.
# TYPE lokistack_info gauge
lokistack_info{size="1x.small",stack_name="test-stack",stack_namespace="test-namespace"} 1
# HELP lokistack_status_condition Counts the current status conditions of the LokiStack.
# TYPE lokistack_status_condition gauge
lokistack_status_condition{condition="Degraded",reason="",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Degraded",reason="",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
lokistack_status_condition{condition="Failed",reason="",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Failed",reason="",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
lokistack_status_condition{condition="Pending",reason="",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Pending",reason="",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
lokistack_status_condition{condition="Ready",reason="",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Ready",reason="",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
lokistack_status_condition{condition="Warning",reason="StorageNeedsSchemaUpdate",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 0
lokistack_status_condition{condition="Warning",reason="StorageNeedsSchemaUpdate",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 1
# HELP lokistack_storage_info Information about LokiStack storage backend configuration.
# TYPE lokistack_storage_info gauge
lokistack_storage_info{backend_type="s3",credential_mode="static",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace"} 1
`,
		},
		{
			desc:     "multiple conditions, inactive warning",
			k8sError: nil,
			stacks: &lokiv1.LokiStackList{
				Items: []lokiv1.LokiStack{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "test-stack",
							Namespace: "test-namespace",
						},
						Spec: lokiv1.LokiStackSpec{
							Size: lokiv1.SizeOneXSmall,
							Storage: lokiv1.ObjectStorageSpec{
								Secret: lokiv1.ObjectStorageSecretSpec{
									Type: lokiv1.ObjectStorageSecretS3,
									Name: "storage-secret",
								},
							},
						},
						Status: lokiv1.LokiStackStatus{
							Conditions: []metav1.Condition{
								{
									Type:   string(lokiv1.ConditionReady),
									Status: metav1.ConditionTrue,
									Reason: string(lokiv1.ReasonReadyComponents),
								},
								{
									Type:   string(lokiv1.ConditionPending),
									Status: metav1.ConditionFalse,
									Reason: string(lokiv1.ReasonPendingComponents),
								},
								{
									Type:   string(lokiv1.ConditionWarning),
									Status: metav1.ConditionFalse,
									Reason: string(lokiv1.ReasonStorageNeedsSchemaUpdate),
								},
							},
						},
					},
				},
			},
			wantMetrics: `# HELP lokistack_info Information about deployed LokiStack instances. Value is always 1.
# TYPE lokistack_info gauge
lokistack_info{size="1x.small",stack_name="test-stack",stack_namespace="test-namespace"} 1
# HELP lokistack_status_condition Counts the current status conditions of the LokiStack.
# TYPE lokistack_status_condition gauge
lokistack_status_condition{condition="Degraded",reason="",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Degraded",reason="",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
lokistack_status_condition{condition="Failed",reason="",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Failed",reason="",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
lokistack_status_condition{condition="Pending",reason="PendingComponents",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Pending",reason="PendingComponents",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
lokistack_status_condition{condition="Ready",reason="ReadyComponents",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 0
lokistack_status_condition{condition="Ready",reason="ReadyComponents",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 1
lokistack_status_condition{condition="Warning",reason="StorageNeedsSchemaUpdate",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Warning",reason="StorageNeedsSchemaUpdate",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
# HELP lokistack_storage_info Information about LokiStack storage backend configuration.
# TYPE lokistack_storage_info gauge
lokistack_storage_info{backend_type="s3",credential_mode="static",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace"} 1
`,
		},
		{
			desc:     "stack with storage config and multiple schemas",
			k8sError: nil,
			stacks: &lokiv1.LokiStackList{
				Items: []lokiv1.LokiStack{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "storage-stack",
							Namespace: "test-ns",
						},
						Spec: lokiv1.LokiStackSpec{
							Size: lokiv1.SizeOneXSmall,
							Storage: lokiv1.ObjectStorageSpec{
								Secret: lokiv1.ObjectStorageSecretSpec{
									Type:           lokiv1.ObjectStorageSecretS3,
									Name:           "s3-secret",
									CredentialMode: lokiv1.CredentialModeToken,
								},
								Schemas: []lokiv1.ObjectStorageSchema{
									{
										Version:       lokiv1.ObjectStorageSchemaV12,
										EffectiveDate: "2024-01-01",
									},
									{
										Version:       lokiv1.ObjectStorageSchemaV13,
										EffectiveDate: "2025-01-01",
									},
								},
							},
						},
						Status: lokiv1.LokiStackStatus{
							Storage: lokiv1.LokiStackStorageStatus{
								CredentialMode: lokiv1.CredentialModeToken,
							},
						},
					},
				},
			},
			wantMetrics: `# HELP lokistack_info Information about deployed LokiStack instances. Value is always 1.
# TYPE lokistack_info gauge
lokistack_info{size="1x.small",stack_name="storage-stack",stack_namespace="test-ns"} 1
# HELP lokistack_status_condition Counts the current status conditions of the LokiStack.
# TYPE lokistack_status_condition gauge
lokistack_status_condition{condition="Degraded",reason="",size="1x.small",stack_name="storage-stack",stack_namespace="test-ns",status="false"} 1
lokistack_status_condition{condition="Degraded",reason="",size="1x.small",stack_name="storage-stack",stack_namespace="test-ns",status="true"} 0
lokistack_status_condition{condition="Failed",reason="",size="1x.small",stack_name="storage-stack",stack_namespace="test-ns",status="false"} 1
lokistack_status_condition{condition="Failed",reason="",size="1x.small",stack_name="storage-stack",stack_namespace="test-ns",status="true"} 0
lokistack_status_condition{condition="Pending",reason="",size="1x.small",stack_name="storage-stack",stack_namespace="test-ns",status="false"} 1
lokistack_status_condition{condition="Pending",reason="",size="1x.small",stack_name="storage-stack",stack_namespace="test-ns",status="true"} 0
lokistack_status_condition{condition="Ready",reason="",size="1x.small",stack_name="storage-stack",stack_namespace="test-ns",status="false"} 1
lokistack_status_condition{condition="Ready",reason="",size="1x.small",stack_name="storage-stack",stack_namespace="test-ns",status="true"} 0
# HELP lokistack_storage_info Information about LokiStack storage backend configuration.
# TYPE lokistack_storage_info gauge
lokistack_storage_info{backend_type="s3",credential_mode="token",size="1x.small",stack_name="storage-stack",stack_namespace="test-ns"} 1
# HELP lokistack_storage_schema_version Storage schema versions configured for the LokiStack
# TYPE lokistack_storage_schema_version gauge
lokistack_storage_schema_version{effective_date="2024-01-01",size="1x.small",stack_name="storage-stack",stack_namespace="test-ns",version="v12"} 1
lokistack_storage_schema_version{effective_date="2025-01-01",size="1x.small",stack_name="storage-stack",stack_namespace="test-ns",version="v13"} 1
`,
		},
		{
			desc:     "stack with custom replicas",
			k8sError: nil,
			stacks: &lokiv1.LokiStackList{
				Items: []lokiv1.LokiStack{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "custom-stack",
							Namespace: "test-ns",
						},
						Spec: lokiv1.LokiStackSpec{
							Size: lokiv1.SizeOneXSmall,
							Storage: lokiv1.ObjectStorageSpec{
								Secret: lokiv1.ObjectStorageSecretSpec{
									Type: lokiv1.ObjectStorageSecretGCS,
									Name: "gcs-secret",
								},
								Schemas: []lokiv1.ObjectStorageSchema{
									{
										Version:       lokiv1.ObjectStorageSchemaV13,
										EffectiveDate: "2025-01-01",
									},
								},
							},
							Template: &lokiv1.LokiTemplateSpec{
								Ingester: &lokiv1.LokiComponentSpec{
									Replicas: 5,
								},
								Querier: &lokiv1.LokiComponentSpec{
									Replicas: 4,
								},
							},
						},
					},
				},
			},
			wantMetrics: `# HELP lokistack_component_replicas Replica count for components (only when different from size defaults)
# TYPE lokistack_component_replicas gauge
lokistack_component_replicas{component="ingester",size="1x.small",stack_name="custom-stack",stack_namespace="test-ns"} 5
lokistack_component_replicas{component="querier",size="1x.small",stack_name="custom-stack",stack_namespace="test-ns"} 4
# HELP lokistack_info Information about deployed LokiStack instances. Value is always 1.
# TYPE lokistack_info gauge
lokistack_info{size="1x.small",stack_name="custom-stack",stack_namespace="test-ns"} 1
# HELP lokistack_status_condition Counts the current status conditions of the LokiStack.
# TYPE lokistack_status_condition gauge
lokistack_status_condition{condition="Degraded",reason="",size="1x.small",stack_name="custom-stack",stack_namespace="test-ns",status="false"} 1
lokistack_status_condition{condition="Degraded",reason="",size="1x.small",stack_name="custom-stack",stack_namespace="test-ns",status="true"} 0
lokistack_status_condition{condition="Failed",reason="",size="1x.small",stack_name="custom-stack",stack_namespace="test-ns",status="false"} 1
lokistack_status_condition{condition="Failed",reason="",size="1x.small",stack_name="custom-stack",stack_namespace="test-ns",status="true"} 0
lokistack_status_condition{condition="Pending",reason="",size="1x.small",stack_name="custom-stack",stack_namespace="test-ns",status="false"} 1
lokistack_status_condition{condition="Pending",reason="",size="1x.small",stack_name="custom-stack",stack_namespace="test-ns",status="true"} 0
lokistack_status_condition{condition="Ready",reason="",size="1x.small",stack_name="custom-stack",stack_namespace="test-ns",status="false"} 1
lokistack_status_condition{condition="Ready",reason="",size="1x.small",stack_name="custom-stack",stack_namespace="test-ns",status="true"} 0
# HELP lokistack_storage_info Information about LokiStack storage backend configuration.
# TYPE lokistack_storage_info gauge
lokistack_storage_info{backend_type="gcs",credential_mode="static",size="1x.small",stack_name="custom-stack",stack_namespace="test-ns"} 1
# HELP lokistack_storage_schema_version Storage schema versions configured for the LokiStack
# TYPE lokistack_storage_schema_version gauge
lokistack_storage_schema_version{effective_date="2025-01-01",size="1x.small",stack_name="custom-stack",stack_namespace="test-ns",version="v13"} 1
`,
		},
		{
			desc:     "stack with ingestion limit",
			k8sError: nil,
			stacks: &lokiv1.LokiStackList{
				Items: []lokiv1.LokiStack{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:      "full-stack",
							Namespace: "logging",
						},
						Spec: lokiv1.LokiStackSpec{
							Size: lokiv1.SizeOneXMedium,
							Storage: lokiv1.ObjectStorageSpec{
								Secret: lokiv1.ObjectStorageSecretSpec{
									Type:           lokiv1.ObjectStorageSecretS3,
									Name:           "s3-secret",
									CredentialMode: lokiv1.CredentialModeTokenCCO,
								},
							},
							Limits: &lokiv1.LimitsSpec{
								Global: &lokiv1.LimitsTemplateSpec{
									IngestionLimits: &lokiv1.IngestionLimitSpec{
										IngestionRate: 100,
									},
								},
							},
						},
						Status: lokiv1.LokiStackStatus{
							Storage: lokiv1.LokiStackStorageStatus{
								CredentialMode: lokiv1.CredentialModeTokenCCO,
							},
						},
					},
				},
			},
			wantMetrics: `# HELP lokistack_global_ingestion_rate_limit_mb Global ingestion rate limit in MB/s.
# TYPE lokistack_global_ingestion_rate_limit_mb gauge
lokistack_global_ingestion_rate_limit_mb{size="1x.medium",stack_name="full-stack",stack_namespace="logging"} 100
# HELP lokistack_info Information about deployed LokiStack instances. Value is always 1.
# TYPE lokistack_info gauge
lokistack_info{size="1x.medium",stack_name="full-stack",stack_namespace="logging"} 1
# HELP lokistack_status_condition Counts the current status conditions of the LokiStack.
# TYPE lokistack_status_condition gauge
lokistack_status_condition{condition="Degraded",reason="",size="1x.medium",stack_name="full-stack",stack_namespace="logging",status="false"} 1
lokistack_status_condition{condition="Degraded",reason="",size="1x.medium",stack_name="full-stack",stack_namespace="logging",status="true"} 0
lokistack_status_condition{condition="Failed",reason="",size="1x.medium",stack_name="full-stack",stack_namespace="logging",status="false"} 1
lokistack_status_condition{condition="Failed",reason="",size="1x.medium",stack_name="full-stack",stack_namespace="logging",status="true"} 0
lokistack_status_condition{condition="Pending",reason="",size="1x.medium",stack_name="full-stack",stack_namespace="logging",status="false"} 1
lokistack_status_condition{condition="Pending",reason="",size="1x.medium",stack_name="full-stack",stack_namespace="logging",status="true"} 0
lokistack_status_condition{condition="Ready",reason="",size="1x.medium",stack_name="full-stack",stack_namespace="logging",status="false"} 1
lokistack_status_condition{condition="Ready",reason="",size="1x.medium",stack_name="full-stack",stack_namespace="logging",status="true"} 0
# HELP lokistack_storage_info Information about LokiStack storage backend configuration.
# TYPE lokistack_storage_info gauge
lokistack_storage_info{backend_type="s3",credential_mode="token-cco",size="1x.medium",stack_name="full-stack",stack_namespace="logging"} 1
`,
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			logger := log.NewLogger("test", log.WithOutput(io.Discard))
			k := &k8sfakes.FakeClient{}
			k.ListStub = func(_ context.Context, list client.ObjectList, _ ...client.ListOption) error {
				if tc.k8sError != nil {
					return tc.k8sError
				}

				k.SetClientObjectList(list, tc.stacks)
				return nil
			}

			expected := strings.NewReader(tc.wantMetrics)

			c := &lokiStackCollector{
				log:       logger,
				k8sClient: k,
			}

			if err := testutil.CollectAndCompare(c, expected); err != nil {
				t.Error(err)
			}
		})
	}
}
