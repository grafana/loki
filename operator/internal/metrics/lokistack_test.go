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
						},
					},
				},
			},
			wantMetrics: `# HELP lokistack_info Information about deployed LokiStack instances. Value is always 1.
# TYPE lokistack_info gauge
lokistack_info{size="1x.demo",stack_name="test-stack",stack_namespace="test-namespace"} 1
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
lokistack_status_condition{condition="Warning",reason="StorageNeedsSchemaUpdate",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 0
lokistack_status_condition{condition="Warning",reason="StorageNeedsSchemaUpdate",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 1
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
lokistack_status_condition{condition="Pending",reason="PendingComponents",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Pending",reason="PendingComponents",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
lokistack_status_condition{condition="Ready",reason="ReadyComponents",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 0
lokistack_status_condition{condition="Ready",reason="ReadyComponents",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 1
lokistack_status_condition{condition="Warning",reason="StorageNeedsSchemaUpdate",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="false"} 1
lokistack_status_condition{condition="Warning",reason="StorageNeedsSchemaUpdate",size="1x.small",stack_name="test-stack",stack_namespace="test-namespace",status="true"} 0
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
