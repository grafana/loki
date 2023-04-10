package status

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
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

func TestGenerateConditions(t *testing.T) {
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

			condition := generateCondition(tc.componentStatus)
			require.Equal(t, tc.wantCondition, condition)
		})
	}
}
