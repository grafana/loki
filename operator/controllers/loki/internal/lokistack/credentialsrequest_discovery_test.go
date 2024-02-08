package lokistack

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/manifests/storage"
)

func TestAnnotateForCredentialsRequest_ReturnError_WhenLokiStackMissing(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	annotationVal := "ns-my-stack-aws-creds"
	stackKey := client.ObjectKey{Name: "my-stack", Namespace: "ns"}

	k.GetStub = func(_ context.Context, _ types.NamespacedName, out client.Object, _ ...client.GetOption) error {
		return apierrors.NewBadRequest("failed to get lokistack")
	}

	err := AnnotateForCredentialsRequest(context.Background(), k, stackKey, annotationVal)
	require.Error(t, err)
}

func TestAnnotateForCredentialsRequest_DoNothing_WhenAnnotationExists(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	annotationVal := "ns-my-stack-aws-creds"
	s := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "ns",
			Annotations: map[string]string{
				storage.AnnotationCredentialsRequestsSecretRef: annotationVal,
			},
		},
	}
	stackKey := client.ObjectKeyFromObject(s)

	k.GetStub = func(_ context.Context, key types.NamespacedName, out client.Object, _ ...client.GetOption) error {
		if key.Name == stackKey.Name && key.Namespace == stackKey.Namespace {
			k.SetClientObject(out, s)
			return nil
		}
		return nil
	}

	err := AnnotateForCredentialsRequest(context.Background(), k, stackKey, annotationVal)
	require.NoError(t, err)
	require.Equal(t, 0, k.UpdateCallCount())
}

func TestAnnotateForCredentialsRequest_UpdateLokistack_WhenAnnotationMissing(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	annotationVal := "ns-my-stack-aws-creds"
	s := &lokiv1.LokiStack{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "my-stack",
			Namespace:   "ns",
			Annotations: map[string]string{},
		},
	}
	stackKey := client.ObjectKeyFromObject(s)

	k.GetStub = func(_ context.Context, key types.NamespacedName, out client.Object, _ ...client.GetOption) error {
		if key.Name == stackKey.Name && key.Namespace == stackKey.Namespace {
			k.SetClientObject(out, s)
			return nil
		}
		return nil
	}

	k.UpdateStub = func(_ context.Context, o client.Object, _ ...client.UpdateOption) error {
		stack, ok := o.(*lokiv1.LokiStack)
		if !ok {
			return apierrors.NewBadRequest("failed conversion to *lokiv1.LokiStack")
		}
		val, ok := stack.Annotations[storage.AnnotationCredentialsRequestsSecretRef]
		if !ok {
			return apierrors.NewBadRequest("missing annotation")
		}
		if val != annotationVal {
			return apierrors.NewBadRequest("annotations does not match input")
		}
		return nil
	}

	err := AnnotateForCredentialsRequest(context.Background(), k, stackKey, annotationVal)
	require.NoError(t, err)
	require.Equal(t, 1, k.UpdateCallCount())
}
