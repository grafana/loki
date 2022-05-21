package handlers_test

import (
	"context"
	"errors"
	"flag"
	"io/ioutil"
	"os"
	"testing"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/handlers"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/status"

	"github.com/ViaQ/logerr/v2/log"
	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
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

var (
	logger logr.Logger

	scheme = runtime.NewScheme()
	flags  = manifests.FeatureFlags{
		EnableCertificateSigningService: false,
		EnableServiceMonitors:           false,
		EnableTLSServiceMonitorConfig:   false,
	}

	defaultSecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-stack-secret",
			Namespace: "some-ns",
		},
		Data: map[string][]byte{
			"endpoint":          []byte("s3://your-endpoint"),
			"region":            []byte("a-region"),
			"bucketnames":       []byte("bucket1,bucket2"),
			"access_key_id":     []byte("a-secret-id"),
			"access_key_secret": []byte("a-secret-key"),
		},
	}

	defaultGatewaySecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-stack-gateway-secret",
			Namespace: "some-ns",
		},
		Data: map[string][]byte{
			"clientID":     []byte("client-123"),
			"clientSecret": []byte("client-secret-xyz"),
			"issuerCAPath": []byte("/tmp/test/ca.pem"),
		},
	}

	invalidSecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-stack-secret",
			Namespace: "some-ns",
		},
		Data: map[string][]byte{},
	}
)

func TestMain(m *testing.M) {
	testing.Init()
	flag.Parse()

	if testing.Verbose() {
		logger = log.NewLogger("testing", log.WithVerbosity(5))
	} else {
		logger = log.NewLogger("testing", log.WithOutput(ioutil.Discard))
	}

	// Register the clientgo and CRD schemes
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(lokiv1beta1.AddToScheme(scheme))

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

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, flags)
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

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, flags)

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

	stack := lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Storage: lokiv1beta1.ObjectStorageSpec{
				Secret: lokiv1beta1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1beta1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1beta1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1beta1.OIDCSpec{
							Secret: &lokiv1beta1.TenantSecretSpec{
								Name: defaultGatewaySecret.Name,
							},
						},
					},
				},
				Authorization: &lokiv1beta1.AuthorizationSpec{
					OPA: &lokiv1beta1.OPASpec{
						URL: "some-url",
					},
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, out client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(out, &stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(out, &defaultSecret)
			return nil
		}
		if defaultGatewaySecret.Name == name.Name {
			k.SetClientObject(out, &defaultGatewaySecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.CreateStub = func(_ context.Context, o client.Object, _ ...client.CreateOption) error {
		assert.Equal(t, r.Namespace, o.GetNamespace())
		return nil
	}

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, flags)
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
		Spec: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Storage: lokiv1beta1.ObjectStorageSpec{
				Secret: lokiv1beta1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1beta1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1beta1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1beta1.OIDCSpec{
							Secret: &lokiv1beta1.TenantSecretSpec{
								Name: defaultGatewaySecret.Name,
							},
						},
					},
				},
				Authorization: &lokiv1beta1.AuthorizationSpec{
					OPA: &lokiv1beta1.OPASpec{
						URL: "some-url",
					},
				},
			},
		},
	}

	// Create looks up the CR first, so we need to return our fake stack
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		if defaultGatewaySecret.Name == name.Name {
			k.SetClientObject(object, &defaultGatewaySecret)
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

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, flags)
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
		Spec: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Storage: lokiv1beta1.ObjectStorageSpec{
				Secret: lokiv1beta1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1beta1.ObjectStorageSecretS3,
				},
			},
		},
	}

	// Create looks up the CR first, so we need to return our fake stack
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
		}
		return nil
	}

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, flags)

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
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Storage: lokiv1beta1.ObjectStorageSpec{
				Secret: lokiv1beta1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1beta1.ObjectStorageSecretS3,
				},
			},
		},
	}

	svc := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack-gossip-ring",
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
					APIVersion:         "loki.grafana.com/v1beta1",
					Kind:               "LokiStack",
					Name:               "my-stack",
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
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
		}
		if svc.Name == name.Name && svc.Namespace == name.Namespace {
			k.SetClientObject(object, &svc)
		}
		return nil
	}

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, flags)
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
		Spec: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Storage: lokiv1beta1.ObjectStorageSpec{
				Secret: lokiv1beta1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1beta1.ObjectStorageSecretS3,
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	// CreateStub returns an error for each resource to trigger reconciliation a new.
	k.CreateStub = func(_ context.Context, o client.Object, _ ...client.CreateOption) error {
		return apierrors.NewTooManyRequestsError("too many create requests")
	}

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, flags)

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
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Storage: lokiv1beta1.ObjectStorageSpec{
				Secret: lokiv1beta1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1beta1.ObjectStorageSecretS3,
				},
			},
		},
	}

	svc := corev1.Service{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Service",
			APIVersion: "v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack-gossip-ring",
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
					APIVersion:         "loki.grafana.com/v1beta1",
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
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
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

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, flags)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
}

func TestCreateOrUpdateLokiStack_WhenMissingSecret_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Missing object storage secret",
		Reason:  lokiv1beta1.ReasonMissingObjectStorageSecret,
		Requeue: false,
	}

	stack := &lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Storage: lokiv1beta1.ObjectStorageSpec{
				Secret: lokiv1beta1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1beta1.ObjectStorageSecretS3,
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, stack)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, flags)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestCreateOrUpdateLokiStack_WhenInvalidSecret_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid object storage secret contents: missing secret field",
		Reason:  lokiv1beta1.ReasonInvalidObjectStorageSecret,
		Requeue: false,
	}

	stack := &lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Storage: lokiv1beta1.ObjectStorageSpec{
				Secret: lokiv1beta1.ObjectStorageSecretSpec{
					Name: invalidSecret.Name,
					Type: lokiv1beta1.ObjectStorageSecretS3,
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, stack)
			return nil
		}
		if name.Name == invalidSecret.Name {
			k.SetClientObject(object, &invalidSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, flags)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestCreateOrUpdateLokiStack_WhenInvalidTenantsConfiguration_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid tenants configuration: mandatory configuration - missing OPA Url",
		Reason:  lokiv1beta1.ReasonInvalidTenantsConfiguration,
		Requeue: false,
	}

	ff := manifests.FeatureFlags{
		EnableGateway: true,
	}

	stack := &lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Storage: lokiv1beta1.ObjectStorageSpec{
				Secret: lokiv1beta1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1beta1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1beta1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1beta1.OIDCSpec{
							Secret: &lokiv1beta1.TenantSecretSpec{
								Name: defaultGatewaySecret.Name,
							},
						},
					},
				},
				Authorization: nil,
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, ff)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestCreateOrUpdateLokiStack_WhenMissingGatewaySecret_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Missing secrets for tenant test",
		Reason:  lokiv1beta1.ReasonMissingGatewayTenantSecret,
		Requeue: true,
	}

	ff := manifests.FeatureFlags{
		EnableGateway: true,
	}

	stack := &lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Storage: lokiv1beta1.ObjectStorageSpec{
				Secret: lokiv1beta1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1beta1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1beta1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1beta1.OIDCSpec{
							Secret: &lokiv1beta1.TenantSecretSpec{
								Name: defaultGatewaySecret.Name,
							},
						},
					},
				},
				Authorization: &lokiv1beta1.AuthorizationSpec{
					OPA: &lokiv1beta1.OPASpec{
						URL: "some-url",
					},
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		o, ok := object.(*lokiv1beta1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && ok {
			k.SetClientObject(o, stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, ff)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestCreateOrUpdateLokiStack_WhenInvalidGatewaySecret_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid gateway tenant secret contents",
		Reason:  lokiv1beta1.ReasonInvalidGatewayTenantSecret,
		Requeue: true,
	}

	ff := manifests.FeatureFlags{
		EnableGateway: true,
	}

	stack := &lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Storage: lokiv1beta1.ObjectStorageSpec{
				Secret: lokiv1beta1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1beta1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1beta1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1beta1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1beta1.OIDCSpec{
							Secret: &lokiv1beta1.TenantSecretSpec{
								Name: invalidSecret.Name,
							},
						},
					},
				},
				Authorization: &lokiv1beta1.AuthorizationSpec{
					OPA: &lokiv1beta1.OPASpec{
						URL: "some-url",
					},
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		o, ok := object.(*lokiv1beta1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && ok {
			k.SetClientObject(o, stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		if name.Name == invalidSecret.Name {
			k.SetClientObject(object, &invalidSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, ff)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestCreateOrUpdateLokiStack_MissingTenantsSpec_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid tenants configuration - TenantsSpec cannot be nil when gateway flag is enabled",
		Reason:  lokiv1beta1.ReasonInvalidTenantsConfiguration,
		Requeue: false,
	}

	ff := manifests.FeatureFlags{
		EnableGateway: true,
	}

	stack := &lokiv1beta1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1beta1.LokiStackSpec{
			Size: lokiv1beta1.SizeOneXExtraSmall,
			Storage: lokiv1beta1.ObjectStorageSpec{
				Secret: lokiv1beta1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1beta1.ObjectStorageSecretS3,
				},
			},
			Tenants: nil,
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object) error {
		o, ok := object.(*lokiv1beta1.LokiStack)
		if r.Name == name.Name && r.Namespace == name.Namespace && ok {
			k.SetClientObject(o, stack)
			return nil
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, ff)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}
