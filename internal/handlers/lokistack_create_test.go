package handlers_test

import (
	"context"
	"errors"
	"flag"
	"io/ioutil"
	"os"
	"testing"

	"github.com/ViaQ/logerr/log"
	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/external/k8s/k8sfakes"
	"github.com/ViaQ/loki-operator/internal/handlers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestMain(m *testing.M) {
	testing.Init()
	flag.Parse()

	if testing.Verbose() {
		// set to the highest for verbose testing
		log.SetLogLevel(5)
	} else {
		if err := log.SetOutput(ioutil.Discard); err != nil {
			// This would only happen if the default logger was changed which it hasn't so
			// we can assume that a panic is necessary and the developer is to blame.
			panic(err)
		}
	}
	log.Init("testing")
	os.Exit(m.Run())
}

func TestCreateLokiStack_WhenGetReturnsNotFound_DoesNotError(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	err := handlers.CreateLokiStack(context.TODO(), r, k)
	require.NoError(t, err)

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCreateLokiStack_WhenGetReturnsAnErrorOtherThanNotFound_ReturnsTheError(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	badRequestErr := apierrors.NewBadRequest("you do not belong here")
	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object) error {
		return badRequestErr
	}

	err := handlers.CreateLokiStack(context.TODO(), r, k)

	require.Equal(t, badRequestErr, errors.Unwrap(err))

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCreateLokiStack_SetsNamespaceOnAllObjects(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.CreateStub = func(_ context.Context, o client.Object, _ ...client.CreateOption) error {
		assert.Equal(t, r.Namespace, o.GetNamespace())
		return nil
	}

	err := handlers.CreateLokiStack(context.TODO(), r, k)
	require.NoError(t, err)

	// make sure create was called
	require.NotZero(t, k.CreateCallCount())
}

func TestCreateLokiStack_SetsOwnerRefOnAllObjects(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "someKind",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someStack",
			Namespace: "someNamespace",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
	}

	// Create looks up the CR first, so we need to return our fake stack
	k.GetStub = func(_ context.Context, _ types.NamespacedName, object client.Object) error {
		k.SetClientObject(object, &stack)
		return nil
	}

	expected := metav1.OwnerReference{
		APIVersion: lokiv1beta1.GroupVersion.String(),
		Kind:       stack.Kind,
		Name:       stack.Name,
		UID:        stack.UID,
		Controller: pointer.BoolPtr(true),
	}

	k.CreateStub = func(_ context.Context, o client.Object, _ ...client.CreateOption) error {
		// OwnerRefs are appended so we have to find ours in the list
		var ref metav1.OwnerReference
		var found bool
		for _, or := range o.GetOwnerReferences() {
			if or.UID == stack.UID {
				found = true
				ref = or
				break
			}
		}

		require.True(t, found, "expected to find a matching ownerRef, but did not")
		require.EqualValues(t, expected, ref)
		return nil
	}

	err := handlers.CreateLokiStack(context.TODO(), r, k)
	require.NoError(t, err)

	// make sure create was called
	require.NotZero(t, k.CreateCallCount())
}
