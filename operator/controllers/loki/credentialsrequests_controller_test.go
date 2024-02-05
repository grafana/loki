package controllers

import (
	"context"
	"testing"

	cloudcredentialsv1 "github.com/openshift/cloud-credential-operator/pkg/apis/cloudcredential/v1"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestCredentialsRequestController_RegistersCustomResource_WithDefaultPredicates(t *testing.T) {
	b := &k8sfakes.FakeBuilder{}
	k := &k8sfakes.FakeClient{}
	c := &CredentialsRequestsReconciler{Client: k, Scheme: scheme}

	b.ForReturns(b)
	b.OwnsReturns(b)

	err := c.buildController(b)
	require.NoError(t, err)

	// Require only one For-Call for the custom resource
	require.Equal(t, 1, b.ForCallCount())

	// Require For-call with LokiStack resource
	obj, _ := b.ForArgsForCall(0)
	require.Equal(t, &lokiv1.LokiStack{}, obj)
}

func TestCredentialsRequestController_DeleteCredentialsRequest_WhenLokiStackNotFound(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	c := &CredentialsRequestsReconciler{Client: k, Scheme: scheme}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "ns",
		},
	}

	// Set managed auth environment
	t.Setenv("ROLEARN", "a-role-arn")

	k.GetStub = func(_ context.Context, key types.NamespacedName, _ client.Object, _ ...client.GetOption) error {
		if key.Name == r.Name && key.Namespace == r.Namespace {
			return apierrors.NewNotFound(schema.GroupResource{}, "lokistack not found")
		}
		return nil
	}

	res, err := c.Reconcile(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, res)
	require.Equal(t, 1, k.DeleteCallCount())
}

func TestCredentialsRequestController_CreateCredentialsRequest_WhenLokiStackNotAnnotated(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	c := &CredentialsRequestsReconciler{Client: k, Scheme: scheme}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "ns",
		},
	}
	s := lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "ns",
		},
		Spec: lokiv1.LokiStackSpec{
			ManagementState: lokiv1.ManagementStateManaged,
		},
	}

	// Set managed auth environment
	t.Setenv("ROLEARN", "a-role-arn")

	k.GetStub = func(_ context.Context, key types.NamespacedName, out client.Object, _ ...client.GetOption) error {
		if key.Name == r.Name && key.Namespace == r.Namespace {
			k.SetClientObject(out, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "lokistack not found")
	}

	k.CreateStub = func(_ context.Context, o client.Object, _ ...client.CreateOption) error {
		_, isCredReq := o.(*cloudcredentialsv1.CredentialsRequest)
		if !isCredReq {
			return apierrors.NewBadRequest("something went wrong creating a credentials request")
		}
		return nil
	}

	k.UpdateStub = func(_ context.Context, o client.Object, _ ...client.UpdateOption) error {
		stack, ok := o.(*lokiv1.LokiStack)
		if !ok {
			return apierrors.NewBadRequest("something went wrong creating a credentials request")
		}

		_, hasSecretRef := stack.Annotations[storage.AnnotationCredentialsRequestsSecretRef]
		if !hasSecretRef {
			return apierrors.NewBadRequest("something went updating the lokistack annotations")
		}
		return nil
	}

	res, err := c.Reconcile(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, res)
	require.Equal(t, 1, k.CreateCallCount())
	require.Equal(t, 1, k.UpdateCallCount())
}

func TestCredentialsRequestController_SkipsUnmanaged(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	c := &CredentialsRequestsReconciler{Client: k, Scheme: scheme}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "ns",
		},
	}

	s := lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "ns",
		},
		Spec: lokiv1.LokiStackSpec{
			ManagementState: lokiv1.ManagementStateUnmanaged,
		},
	}

	k.GetStub = func(_ context.Context, key types.NamespacedName, out client.Object, _ ...client.GetOption) error {
		if key.Name == s.Name && key.Namespace == s.Namespace {
			k.SetClientObject(out, &s)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something not found")
	}

	res, err := c.Reconcile(context.Background(), r)
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, res)
}
