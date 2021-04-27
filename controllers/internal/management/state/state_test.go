package state_test

import (
	"context"
	"testing"

	"github.com/ViaQ/logerr/kverrors"
	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/controllers/internal/management/state"
	"github.com/ViaQ/loki-operator/internal/external/k8s/k8sfakes"
	"github.com/stretchr/testify/require"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestIsManaged(t *testing.T) {
	type test struct {
		name   string
		stack  lokiv1beta1.LokiStack
		wantOk bool
	}

	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}
	table := []test{
		{
			name: "managed",
			stack: lokiv1beta1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1beta1.LokiStackSpec{
					ManagementState: lokiv1beta1.ManagementStateManaged,
				},
			},
			wantOk: true,
		},
		{
			name: "unmanaged",
			stack: lokiv1beta1.LokiStack{
				TypeMeta: metav1.TypeMeta{
					Kind: "LokiStack",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "my-stack",
					Namespace: "some-ns",
					UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
				},
				Spec: lokiv1beta1.LokiStackSpec{
					ManagementState: lokiv1beta1.ManagementStateUnmanaged,
				},
			},
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()
			k.GetStub = func(_ context.Context, _ types.NamespacedName, object client.Object) error {
				k.SetClientObject(object, &tst.stack)
				return nil
			}
			ok, err := state.IsManaged(context.TODO(), r, k)
			require.NoError(t, err)
			require.Equal(t, ok, tst.wantOk)
		})
	}
}

func TestIsManaged_WhenError_ReturnNotManagedWithError(t *testing.T) {
	type test struct {
		name     string
		apierror error
		wantErr  error
	}

	badReqErr := apierrors.NewBadRequest("bad request")
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}
	table := []test{
		{
			name:     "stack not found error",
			apierror: apierrors.NewNotFound(schema.GroupResource{}, "something not found"),
		},
		{
			name:     "any other api error",
			apierror: badReqErr,
			wantErr:  kverrors.Wrap(badReqErr, "failed to lookup lokistack", "name", r.NamespacedName),
		},
	}
	for _, tst := range table {
		tst := tst
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()
			k.GetStub = func(_ context.Context, _ types.NamespacedName, _ client.Object) error {
				return tst.apierror
			}
			ok, err := state.IsManaged(context.TODO(), r, k)
			require.Equal(t, tst.wantErr, err)
			require.False(t, ok)
		})
	}
}
