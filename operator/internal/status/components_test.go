package status_test

import (
	"context"
	"testing"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/status"
	"github.com/stretchr/testify/require"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestSetComponentsStatus_WhenGetLokiStackReturnsError_ReturnError(t *testing.T) {
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

	err := status.SetComponentsStatus(context.TODO(), k, r)
	require.Error(t, err)
}

func TestSetComponentsStatus_WhenGetLokiStackReturnsNotFound_DoNothing(t *testing.T) {
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

	err := status.SetComponentsStatus(context.TODO(), k, r)
	require.NoError(t, err)
}

func TestSetComponentsStatus_WhenListReturnError_ReturnError(t *testing.T) {
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

	k.ListStub = func(_ context.Context, l client.ObjectList, opts ...client.ListOption) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := status.SetComponentsStatus(context.TODO(), k, r)
	require.Error(t, err)
}

func TestSetComponentsStatus_WhenPodListExisting_SetPodStatusMap(t *testing.T) {
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

	k.ListStub = func(_ context.Context, l client.ObjectList, _ ...client.ListOption) error {
		pods := v1.PodList{
			Items: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod-a",
					},
					Status: v1.PodStatus{
						Phase: v1.PodPending,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod-b",
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				},
			},
		}
		k.SetClientObjectList(l, &pods)
		return nil
	}

	expected := lokiv1beta1.PodStatusMap{
		"Pending": []string{"pod-a"},
		"Running": []string{"pod-b"},
	}

	sw.UpdateStub = func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
		stack := obj.(*lokiv1beta1.LokiStack)
		require.Equal(t, expected, stack.Status.Components.Compactor)
		return nil
	}

	err := status.SetComponentsStatus(context.TODO(), k, r)
	require.NoError(t, err)
	require.NotZero(t, k.ListCallCount())
	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}

func TestSetComponentsStatus_WhenRulerEnabled_SetPodStatusMap(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.StatusStub = func() client.StatusWriter { return sw }

	s := lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Rules: &lokiv1beta1.RulesSpec{
				Enabled: true,
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

	k.ListStub = func(_ context.Context, l client.ObjectList, _ ...client.ListOption) error {
		pods := v1.PodList{
			Items: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod-a",
					},
					Status: v1.PodStatus{
						Phase: v1.PodPending,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod-b",
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				},
			},
		}
		k.SetClientObjectList(l, &pods)
		return nil
	}

	expected := lokiv1beta1.PodStatusMap{
		"Pending": []string{"pod-a"},
		"Running": []string{"pod-b"},
	}

	sw.UpdateStub = func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
		stack := obj.(*lokiv1beta1.LokiStack)
		require.Equal(t, expected, stack.Status.Components.Ruler)
		return nil
	}

	err := status.SetComponentsStatus(context.TODO(), k, r)
	require.NoError(t, err)
	require.NotZero(t, k.ListCallCount())
	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}

func TestSetComponentsStatus_WhenRulerNotEnabled_DoNothing(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}

	k.StatusStub = func() client.StatusWriter { return sw }

	s := lokiv1beta1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Rules: &lokiv1beta1.RulesSpec{
				Enabled: false,
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

	k.ListStub = func(_ context.Context, l client.ObjectList, o ...client.ListOption) error {
		s := o[0].(client.MatchingLabels)

		c, ok := s["app.kubernetes.io/component"]
		if !ok || c == "ruler" {
			return nil
		}

		pods := v1.PodList{
			Items: []v1.Pod{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod-a",
					},
					Status: v1.PodStatus{
						Phase: v1.PodPending,
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name: "pod-b",
					},
					Status: v1.PodStatus{
						Phase: v1.PodRunning,
					},
				},
			},
		}
		k.SetClientObjectList(l, &pods)
		return nil
	}

	sw.UpdateStub = func(_ context.Context, obj client.Object, _ ...client.UpdateOption) error {
		stack := obj.(*lokiv1beta1.LokiStack)
		require.Equal(t, stack.Status.Components.Ruler, lokiv1beta1.PodStatusMap{})
		return nil
	}

	err := status.SetComponentsStatus(context.TODO(), k, r)
	require.NoError(t, err)
	require.NotZero(t, k.ListCallCount())
	require.NotZero(t, k.StatusCallCount())
	require.NotZero(t, sw.UpdateCallCount())
}
