package status

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

func setupFakesNoError(t *testing.T, stack *lokiv1.LokiStack) (*k8sfakes.FakeClient, *k8sfakes.FakeStatusWriter) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if name.Name == stack.Name && name.Namespace == stack.Namespace {
			k.SetClientObject(object, stack)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}
	k.StatusStub = func() client.StatusWriter { return sw }

	sw.UpdateStub = func(_ context.Context, obj client.Object, _ ...client.SubResourceUpdateOption) error {
		actual := obj.(*lokiv1.LokiStack)
		require.NotEmpty(t, actual.Status.Conditions)
		require.Equal(t, metav1.ConditionTrue, actual.Status.Conditions[0].Status)
		return nil
	}

	return k, sw
}

func TestGenerateCondition(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	lokiStack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-lokistack",
			Namespace: "test-ns",
		},
	}
	tt := []struct {
		desc            string
		componentStatus *lokiv1.LokiStackComponentStatus
		degradedErr     *DegradedError
		wantCondition   metav1.Condition
	}{
		{
			desc:            "no error",
			componentStatus: &lokiv1.LokiStackComponentStatus{},
			wantCondition:   conditionReady,
		},
		{
			desc: "container pending",
			componentStatus: &lokiv1.LokiStackComponentStatus{
				Ingester: lokiv1.PodStatusMap{
					lokiv1.PodPending: {
						"pod-0",
					},
				},
			},
			wantCondition: conditionPending,
		},
		{
			desc: "container failed",
			componentStatus: &lokiv1.LokiStackComponentStatus{
				Ingester: lokiv1.PodStatusMap{
					lokiv1.PodFailed: {
						"pod-0",
					},
				},
			},
			wantCondition: conditionFailed,
		},
		{
			desc: "degraded error",
			componentStatus: &lokiv1.LokiStackComponentStatus{
				Ingester: lokiv1.PodStatusMap{
					lokiv1.PodRunning: {
						"pod-0",
					},
				},
			},
			degradedErr: &DegradedError{
				Message: "test-message",
				Reason:  "test-reason",
			},
			wantCondition: metav1.Condition{
				Type:    "Degraded",
				Reason:  "test-reason",
				Message: "test-message",
			},
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			condition, err := generateCondition(context.TODO(), tc.componentStatus, k, &lokiStack, tc.degradedErr)
			require.Nil(t, err)
			require.Equal(t, tc.wantCondition, condition)
		})
	}
}

func TestGenerateCondition_ZoneAwareLokiStack(t *testing.T) {
	testError := errors.New("test-error") //nolint:goerr113
	tt := []struct {
		desc          string
		nodes         []corev1.Node
		wantCondition metav1.Condition
		wantErr       error
	}{
		{
			desc: "nodes available",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"topology-key": "value"},
				}},
			},
			wantCondition: conditionPending,
		},
		{
			desc: "nodes available but empty label value",
			nodes: []corev1.Node{
				{ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{"topology-key": ""},
				}},
			},
			wantCondition: conditionDegradedEmptyNodeLabel,
		},
		{
			desc:          "no nodes available",
			nodes:         []corev1.Node{},
			wantCondition: conditionDegradedNodeLabels,
		},
		{
			desc:    "api error",
			nodes:   []corev1.Node{},
			wantErr: testError,
		},
	}

	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			componentStatus := &lokiv1.LokiStackComponentStatus{
				Ingester: lokiv1.PodStatusMap{
					lokiv1.PodPending: {
						"pod-0",
					},
				},
			}
			lokiStack := lokiv1.LokiStack{
				Spec: lokiv1.LokiStackSpec{
					Replication: &lokiv1.ReplicationSpec{
						Zones: []lokiv1.ZoneSpec{
							{
								TopologyKey: "topology-key",
							},
						},
					},
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-lokistack",
					Namespace: "test-ns",
				},
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
			}

			k, _ := setupFakesNoError(t, &lokiStack)
			k.ListStub = func(_ context.Context, ol client.ObjectList, options ...client.ListOption) error {
				for _, o := range options {
					if labels, ok := o.(client.HasLabels); ok {
						require.Len(t, labels, 1)
						require.Equal(t, "topology-key", labels[0])
					}
				}

				k.SetClientObjectList(ol, &corev1.NodeList{
					Items: tc.nodes,
				})
				return tc.wantErr
			}

			condition, err := generateCondition(context.TODO(), componentStatus, k, &lokiStack, nil)

			require.Equal(t, tc.wantErr, err)
			require.Equal(t, tc.wantCondition, condition)
		})
	}
}

func TestGenerateWarningCondition_WhenStorageSchemaIsOld(t *testing.T) {
	tt := []struct {
		desc          string
		schemas       []lokiv1.ObjectStorageSchema
		wantCondition []metav1.Condition
	}{
		{
			desc: "no V13 in schema config",
			schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-11",
				},
				{
					Version:       lokiv1.ObjectStorageSchemaV12,
					EffectiveDate: "2023-10-11",
				},
			},
			wantCondition: []metav1.Condition{{
				Type:    string(lokiv1.ConditionWarning),
				Reason:  string(lokiv1.ReasonStorageNeedsSchemaUpdate),
				Message: messageWarningNeedsSchemaVersionUpdate,
			}},
		},
		{
			desc: "with V13 not as the last element in schema config",
			schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-11",
				},
				{
					Version:       lokiv1.ObjectStorageSchemaV13,
					EffectiveDate: "2023-10-11",
				},
				{
					Version:       lokiv1.ObjectStorageSchemaV12,
					EffectiveDate: "2024-10-11",
				},
			},
			wantCondition: []metav1.Condition{{
				Type:    string(lokiv1.ConditionWarning),
				Reason:  string(lokiv1.ReasonStorageNeedsSchemaUpdate),
				Message: messageWarningNeedsSchemaVersionUpdate,
			}},
		},
		{
			desc: "with V13 as the last element in schema config",
			schemas: []lokiv1.ObjectStorageSchema{
				{
					Version:       lokiv1.ObjectStorageSchemaV11,
					EffectiveDate: "2020-10-11",
				},
				{
					Version:       lokiv1.ObjectStorageSchemaV12,
					EffectiveDate: "2023-10-11",
				},
				{
					Version:       lokiv1.ObjectStorageSchemaV13,
					EffectiveDate: "2024-10-11",
				},
			},
			wantCondition: []metav1.Condition{},
		},
	}
	for _, tc := range tt {
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()
			condition := generateWarnings(tc.schemas)
			require.Equal(t, condition, tc.wantCondition)
		})
	}
}
