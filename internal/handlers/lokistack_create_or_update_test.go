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

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var scheme = runtime.NewScheme()

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

	// Register the clientgo and CRD schemes
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(lokiv1beta1.AddToScheme(scheme))

	log.Init("testing")
	os.Exit(m.Run())
}

func TestCreateOrUpdateLokiStack_WhenGetReturnsNotFound_DoesNotError(t *testing.T) {
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

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), r, k, scheme)
	require.NoError(t, err)

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCreateOrUpdateLokiStack_WhenGetReturnsAnErrorOtherThanNotFound_ReturnsTheError(t *testing.T) {
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

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), r, k, scheme)

	require.Equal(t, badRequestErr, errors.Unwrap(err))

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCreateOrUpdateLokiStack_SetsNamespaceOnAllObjects(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, _ client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.CreateStub = func(_ context.Context, o client.Object, _ ...client.CreateOption) error {
		assert.Equal(t, r.Namespace, o.GetNamespace())
		return nil
	}

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), r, k, scheme)
	require.NoError(t, err)

	// make sure create was called
	require.NotZero(t, k.CreateCallCount())
}

func TestCreateOrUpdateLokiStack_SetsOwnerRefOnAllObjects(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someStack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
	}

	// Create looks up the CR first, so we need to return our fake stack
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	expected := metav1.OwnerReference{
		APIVersion:         lokiv1beta1.GroupVersion.String(),
		Kind:               stack.Kind,
		Name:               stack.Name,
		UID:                stack.UID,
		Controller:         pointer.BoolPtr(true),
		BlockOwnerDeletion: pointer.BoolPtr(true),
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

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), r, k, scheme)
	require.NoError(t, err)

	// make sure create was called
	require.NotZero(t, k.CreateCallCount())
}

func TestCreateOrUpdateLokiStack_WhenSetControllerRefInvalid_ContinueWithOtherObjects(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "someStack",
			// Set invalid namespace here, because
			// because cross-namespace controller
			// references are not allowed
			Namespace: "invalid-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
	}

	// Create looks up the CR first, so we need to return our fake stack
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
		}
		return nil
	}

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), r, k, scheme)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
}

func TestCreateOrUpdateLokiStack_WhenGetReturnsNoError_UpdateObjects(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someStack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
	}

	svc := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "loki-gossip-ring-my-stack",
			Namespace: "some-ns",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "loki",
				"app.kubernetes.io/provider": "openshift",
				"loki.grafana.com/name":      "my-stack",

				// Add custom label to fake semantic not equal
				"test": "test",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "loki.openshift.io/v1beta1",
					Kind:               "LokiStack",
					Name:               "someStack",
					UID:                "b23f9a38-9672-499f-8c29-15ede74d3ece",
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{
					Name:     "gossip",
					Port:     7946,
					Protocol: "TCP",
				},
			},
			Selector: map[string]string{
				"app.kubernetes.io/name":     "loki",
				"app.kubernetes.io/provider": "openshift",
				"loki.grafana.com/name":      "my-stack",
			},
		},
	}

	// Create looks up the CR first, so we need to return our fake stack
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
		}

		if svc.Name == name.Name && svc.Namespace == name.Namespace {
			k.SetClientObject(object, &svc)
		}

		return nil
	}

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), r, k, scheme)
	require.NoError(t, err)

	// make sure create not called
	require.Zero(t, k.CreateCallCount())

	// make sure update was called
	require.NotZero(t, k.UpdateCallCount())
}

func TestCreateOrUpdateLokiStack_WhenCreateReturnsError_ContinueWithOtherObjects(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someStack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	// CreateStub returns an error for each resource to trigger reconciliation a new.
	k.CreateStub = func(_ context.Context, o client.Object, _ ...client.CreateOption) error {
		return apierrors.NewTooManyRequestsError("too many create requests")
	}

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), r, k, scheme)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
}

func TestCreateOrUpdateLokiStack_WhenUpdateReturnsError_ContinueWithOtherObjects(t *testing.T) {
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someStack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
	}

	svc := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "loki-gossip-ring-my-stack",
			Namespace: "some-ns",
			Labels: map[string]string{
				"app.kubernetes.io/name":     "loki",
				"app.kubernetes.io/provider": "openshift",
				"loki.grafana.com/name":      "my-stack",

				// Add custom label to fake semantic not equal
				"test": "test",
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion:         "loki.openshift.io/v1beta1",
					Kind:               "LokiStack",
					Name:               "someStack",
					UID:                "b23f9a38-9672-499f-8c29-15ede74d3ece",
					Controller:         pointer.BoolPtr(true),
					BlockOwnerDeletion: pointer.BoolPtr(true),
				},
			},
		},
		Spec: corev1.ServiceSpec{
			ClusterIP: "None",
			Ports: []corev1.ServicePort{
				{
					Name:     "gossip",
					Port:     7946,
					Protocol: "TCP",
				},
			},
			Selector: map[string]string{
				"app.kubernetes.io/name":     "loki",
				"app.kubernetes.io/provider": "openshift",
				"loki.grafana.com/name":      "my-stack",
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
		}
		if svc.Name == name.Name && svc.Namespace == name.Namespace {
			k.SetClientObject(object, &svc)
		}
		return nil
	}

	// CreateStub returns an error for each resource to trigger reconciliation a new.
	k.UpdateStub = func(_ context.Context, o client.Object, _ ...client.UpdateOption) error {
		return apierrors.NewTooManyRequestsError("too many create requests")
	}

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), r, k, scheme)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
}
