package status

import (
	"context"
	"errors"
	"testing"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func TestSetDegradedCondition_WhenGetLokiStackReturnsNotFound_DoNothing(t *testing.T) {
	msg := "tell me nothing"
	reason := lokiv1.ReasonMissingObjectStorageSecret

	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k := &k8sfakes.FakeClient{}
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := SetDegradedCondition(context.Background(), k, r, msg, reason)
	require.NoError(t, err)
}

func TestSetDegradedCondition_WhenExisting_DoNothing(t *testing.T) {
	msg := "tell me nothing"
	reason := lokiv1.ReasonMissingObjectStorageSecret
	s := lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Status: lokiv1.LokiStackStatus{
			Conditions: []metav1.Condition{
				{
					Type:    string(lokiv1.ConditionDegraded),
					Reason:  string(reason),
					Message: msg,
					Status:  metav1.ConditionTrue,
				},
			},
		},
	}

	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k, _ := setupFakesNoError(t, &s)

	err := SetDegradedCondition(context.Background(), k, r, msg, reason)
	require.NoError(t, err)
	require.Zero(t, k.StatusCallCount())
}

func TestSetDegradedCondition_WhenExisting_SetDegradedConditionTrue(t *testing.T) {
	msg := "tell me something"
	reason := lokiv1.ReasonMissingObjectStorageSecret
	s := lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Status: lokiv1.LokiStackStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(lokiv1.ConditionDegraded),
					Reason: string(reason),
					Status: metav1.ConditionFalse,
				},
			},
		},
	}

	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k, sw := setupFakesNoError(t, &s)

	err := SetDegradedCondition(context.Background(), k, r, msg, reason)
	require.NoError(t, err)
	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}

func TestSetDegradedCondition_WhenNoneExisting_AppendDegradedCondition(t *testing.T) {
	msg := "tell me something"
	reason := lokiv1.ReasonMissingObjectStorageSecret
	s := lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k, sw := setupFakesNoError(t, &s)

	err := SetDegradedCondition(context.Background(), k, r, msg, reason)
	require.NoError(t, err)

	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}

func TestGenerateCondition(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "test-lokistack",
			Namespace: "some-ns",
		},
	}
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
				Ingester: map[corev1.PodPhase][]string{
					corev1.PodPending: {
						"pod-0",
					},
				},
			},
			wantCondition: conditionPending,
		},
		{
			desc: "container failed",
			componentStatus: &lokiv1.LokiStackComponentStatus{
				Ingester: map[corev1.PodPhase][]string{
					corev1.PodFailed: {
						"pod-0",
					},
				},
			},
			wantCondition: conditionFailed,
		},
	}

	for _, tc := range tt {
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			condition, err := generateCondition(context.TODO(), tc.componentStatus, k, r, &lokiStack)
			require.Nil(t, err)
			require.Equal(t, tc.wantCondition, condition)
		})
	}
}

func TestGenerateCondition_ZoneAwareLokiStack(t *testing.T) {
	testError := errors.New("test-error")
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
		tc := tc
		t.Run(tc.desc, func(t *testing.T) {
			t.Parallel()

			r := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Name:      "test-lokistack",
					Namespace: "some-ns",
				},
			}
			componentStatus := &lokiv1.LokiStackComponentStatus{
				Ingester: map[corev1.PodPhase][]string{
					corev1.PodPending: {
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

			condition, err := generateCondition(context.TODO(), componentStatus, k, r, &lokiStack)

			require.Equal(t, tc.wantErr, err)
			require.Equal(t, tc.wantCondition, condition)
		})
	}
}
