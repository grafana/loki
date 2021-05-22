package status_test

import (
	"context"
	"testing"

	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/external/k8s/k8sfakes"
	"github.com/ViaQ/loki-operator/internal/status"

	"github.com/stretchr/testify/require"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSetReadyCondition_WhenGetLokiStackReturnsError_ReturnError(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		return apierrors.NewBadRequest("something wasn't found")
	}

	err := status.SetReadyCondition(context.TODO(), k, r)
	require.Error(t, err)
}

func TestSetReadyCondition_WhenGetLokiStackReturnsNotFound_DoNothing(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := status.SetReadyCondition(context.TODO(), k, r)
	require.NoError(t, err)
}

func TestSetReadyCondition_WhenExisting_DoNothing(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	s := lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Status: lokiv1beta1.LokiStackStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(lokiv1beta1.ConditionReady),
					Status: metav1.ConditionTrue,
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := status.SetReadyCondition(context.TODO(), k, r)
	require.NoError(t, err)
	require.Zero(t, k.StatusCallCount())
}

func TestSetReadyCondition_WhenExisting_SetReadyConditionTrue(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.StatusStub = func() client.StatusWriter { return sw }

	s := lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Status: lokiv1beta1.LokiStackStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(lokiv1beta1.ConditionReady),
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	sw.UpdateStub = func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
		actual := obj.(*lokiv1beta1.LokiStack)
		require.NotEmpty(t, actual.Status.Conditions)
		require.Equal(t, metav1.ConditionTrue, actual.Status.Conditions[0].Status)
		return nil
	}

	err := status.SetReadyCondition(context.TODO(), k, r)
	require.NoError(t, err)

	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}

func TestSetReadyCondition_WhenNoneExisting_AppendReadyCondition(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.StatusStub = func() client.StatusWriter { return sw }

	s := lokiv1beta1.LokiStack{
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	sw.UpdateStub = func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
		actual := obj.(*lokiv1beta1.LokiStack)
		require.NotEmpty(t, actual.Status.Conditions)
		return nil
	}

	err := status.SetReadyCondition(context.TODO(), k, r)
	require.NoError(t, err)

	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}

func TestSetFailedCondition_WhenGetLokiStackReturnsError_ReturnError(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		return apierrors.NewBadRequest("something wasn't found")
	}

	err := status.SetFailedCondition(context.TODO(), k, r)
	require.Error(t, err)
}

func TestSetFailedCondition_WhenGetLokiStackReturnsNotFound_DoNothing(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := status.SetFailedCondition(context.TODO(), k, r)
	require.NoError(t, err)
}

func TestSetFailedCondition_WhenExisting_DoNothing(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	s := lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Status: lokiv1beta1.LokiStackStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(lokiv1beta1.ConditionFailed),
					Status: metav1.ConditionTrue,
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := status.SetFailedCondition(context.TODO(), k, r)
	require.NoError(t, err)
	require.Zero(t, k.StatusCallCount())
}

func TestSetFailedCondition_WhenExisting_SetFailedConditionTrue(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.StatusStub = func() client.StatusWriter { return sw }

	s := lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Status: lokiv1beta1.LokiStackStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(lokiv1beta1.ConditionFailed),
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	sw.UpdateStub = func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
		actual := obj.(*lokiv1beta1.LokiStack)
		require.NotEmpty(t, actual.Status.Conditions)
		require.Equal(t, metav1.ConditionTrue, actual.Status.Conditions[0].Status)
		return nil
	}

	err := status.SetFailedCondition(context.TODO(), k, r)
	require.NoError(t, err)

	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}

func TestSetFailedCondition_WhenNoneExisting_AppendFailedCondition(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.StatusStub = func() client.StatusWriter { return sw }

	s := lokiv1beta1.LokiStack{
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	sw.UpdateStub = func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
		actual := obj.(*lokiv1beta1.LokiStack)
		require.NotEmpty(t, actual.Status.Conditions)
		return nil
	}

	err := status.SetFailedCondition(context.TODO(), k, r)
	require.NoError(t, err)

	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}

func TestSetDegradedCondition_WhenGetLokiStackReturnsError_ReturnError(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	msg := "tell me nothing"
	reason := lokiv1beta1.ReasonMissingObjectStorageSecret

	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		return apierrors.NewBadRequest("something wasn't found")
	}

	err := status.SetDegradedCondition(context.TODO(), k, r, msg, reason)
	require.Error(t, err)
}

func TestSetPendingCondition_WhenGetLokiStackReturnsError_ReturnError(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		return apierrors.NewBadRequest("something wasn't found")
	}

	err := status.SetPendingCondition(context.TODO(), k, r)
	require.Error(t, err)
}

func TestSetPendingCondition_WhenGetLokiStackReturnsNotFound_DoNothing(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := status.SetPendingCondition(context.TODO(), k, r)
	require.NoError(t, err)
}

func TestSetPendingCondition_WhenExisting_DoNothing(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	s := lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Status: lokiv1beta1.LokiStackStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(lokiv1beta1.ConditionPending),
					Status: metav1.ConditionTrue,
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := status.SetPendingCondition(context.TODO(), k, r)
	require.NoError(t, err)
	require.Zero(t, k.StatusCallCount())
}

func TestSetPendingCondition_WhenExisting_SetPendingConditionTrue(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.StatusStub = func() client.StatusWriter { return sw }

	s := lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Status: lokiv1beta1.LokiStackStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(lokiv1beta1.ConditionPending),
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	sw.UpdateStub = func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
		actual := obj.(*lokiv1beta1.LokiStack)
		require.NotEmpty(t, actual.Status.Conditions)
		require.Equal(t, metav1.ConditionTrue, actual.Status.Conditions[0].Status)
		return nil
	}

	err := status.SetPendingCondition(context.TODO(), k, r)
	require.NoError(t, err)
	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}

func TestSetPendingCondition_WhenNoneExisting_AppendPendingCondition(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.StatusStub = func() client.StatusWriter { return sw }

	s := lokiv1beta1.LokiStack{
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	sw.UpdateStub = func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
		actual := obj.(*lokiv1beta1.LokiStack)
		require.NotEmpty(t, actual.Status.Conditions)
		return nil
	}

	err := status.SetPendingCondition(context.TODO(), k, r)
	require.NoError(t, err)

	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}

func TestSetDegradedCondition_WhenGetLokiStackReturnsNotFound_DoNothing(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	msg := "tell me nothing"
	reason := lokiv1beta1.ReasonMissingObjectStorageSecret

	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := status.SetDegradedCondition(context.TODO(), k, r, msg, reason)
	require.NoError(t, err)
}

func TestSetDegradedCondition_WhenExisting_DoNothing(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	msg := "tell me nothing"
	reason := lokiv1beta1.ReasonMissingObjectStorageSecret
	s := lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Status: lokiv1beta1.LokiStackStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(lokiv1beta1.ConditionDegraded),
					Reason: string(reason),
					Status: metav1.ConditionTrue,
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := status.SetDegradedCondition(context.TODO(), k, r, msg, reason)
	require.NoError(t, err)
	require.Zero(t, k.StatusCallCount())
}

func TestSetDegradedCondition_WhenExisting_SetDegradedConditionTrue(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.StatusStub = func() client.StatusWriter { return sw }

	msg := "tell me something"
	reason := lokiv1beta1.ReasonMissingObjectStorageSecret
	s := lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Status: lokiv1beta1.LokiStackStatus{
			Conditions: []metav1.Condition{
				{
					Type:   string(lokiv1beta1.ConditionDegraded),
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	sw.UpdateStub = func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
		actual := obj.(*lokiv1beta1.LokiStack)
		require.NotEmpty(t, actual.Status.Conditions)
		require.Equal(t, metav1.ConditionTrue, actual.Status.Conditions[0].Status)
		return nil
	}

	err := status.SetDegradedCondition(context.TODO(), k, r, msg, reason)
	require.NoError(t, err)
	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}

func TestSetDegradedCondition_WhenNoneExisting_AppendDegradedCondition(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.StatusStub = func() client.StatusWriter { return sw }

	msg := "tell me something"
	reason := lokiv1beta1.ReasonMissingObjectStorageSecret
	s := lokiv1beta1.LokiStack{
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

	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	sw.UpdateStub = func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
		actual := obj.(*lokiv1beta1.LokiStack)
		require.NotEmpty(t, actual.Status.Conditions)
		return nil
	}

	err := status.SetDegradedCondition(context.TODO(), k, r, msg, reason)
	require.NoError(t, err)

	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}
