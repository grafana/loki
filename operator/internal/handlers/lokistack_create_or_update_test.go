package handlers_test

import (
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"testing"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	"github.com/grafana/loki/operator/internal/handlers"
	"github.com/grafana/loki/operator/internal/status"

	"github.com/ViaQ/logerr/v2/log"
	"github.com/go-logr/logr"
	routev1 "github.com/openshift/api/route/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
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

	scheme       = runtime.NewScheme()
	featureGates = configv1.FeatureGates{
		ServiceMonitors:            false,
		ServiceMonitorTLSEndpoints: false,
		BuiltInCertManagement: configv1.BuiltInCertManagement{
			Enabled:        true,
			CACertValidity: "10m",
			CACertRefresh:  "5m",
			CertValidity:   "2m",
			CertRefresh:    "1m",
		},
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

	rulesCM = corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind: "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack-rules-0",
			Namespace: "some-ns",
		},
	}

	rulerSS = appsv1.StatefulSet{
		TypeMeta: metav1.TypeMeta{
			Kind: "StatefulSet",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack-ruler",
			Namespace: "some-ns",
		},
	}

	invalidSecret = corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-stack-secret",
			Namespace: "some-ns",
		},
		Data: map[string][]byte{},
	}

	invalidCAConfigMap = corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "some-stack-ca-configmap",
			Namespace: "some-ns",
		},
		Data: map[string]string{},
	}
)

func TestMain(m *testing.M) {
	testing.Init()
	flag.Parse()

	if testing.Verbose() {
		logger = log.NewLogger("testing", log.WithVerbosity(5))
	} else {
		logger = log.NewLogger("testing", log.WithOutput(io.Discard))
	}

	// Register the clientgo and CRD schemes
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(routev1.AddToScheme(scheme))
	utilruntime.Must(lokiv1.AddToScheme(scheme))

	os.Exit(m.Run())
}

func TestCreateOrUpdateLokiStack_WhenGetReturnsNotFound_DoesNotError(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return apierrors.NewNotFound(schema.GroupResource{}, "something wasn't found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)
	require.NoError(t, err)

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCreateOrUpdateLokiStack_WhenGetReturnsAnErrorOtherThanNotFound_ReturnsTheError(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	badRequestErr := apierrors.NewBadRequest("you do not belong here")
	k.GetStub = func(ctx context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		return badRequestErr
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)

	require.Equal(t, badRequestErr, errors.Unwrap(err))

	// make sure create was NOT called because the Get failed
	require.Zero(t, k.CreateCallCount())
}

func TestCreateOrUpdateLokiStack_SetsNamespaceOnAllObjects(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1.OIDCSpec{
							Secret: &lokiv1.TenantSecretSpec{
								Name: defaultGatewaySecret.Name,
							},
						},
					},
				},
				Authorization: &lokiv1.AuthorizationSpec{
					OPA: &lokiv1.OPASpec{
						URL: "some-url",
					},
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, out client.Object, _ ...client.GetOption) error {
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

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)
	require.NoError(t, err)

	// make sure create was called
	require.NotZero(t, k.CreateCallCount())
}

func TestCreateOrUpdateLokiStack_SetsOwnerRefOnAllObjects(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someStack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1.OIDCSpec{
							Secret: &lokiv1.TenantSecretSpec{
								Name: defaultGatewaySecret.Name,
							},
						},
					},
				},
				Authorization: &lokiv1.AuthorizationSpec{
					OPA: &lokiv1.OPASpec{
						URL: "some-url",
					},
				},
			},
		},
	}

	// Create looks up the CR first, so we need to return our fake stack
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
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
		APIVersion:         lokiv1.GroupVersion.String(),
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

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)
	require.NoError(t, err)

	// make sure create was called
	require.NotZero(t, k.CreateCallCount())
}

func TestCreateOrUpdateLokiStack_WhenSetControllerRefInvalid_ContinueWithOtherObjects(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
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
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
		},
	}

	// Create looks up the CR first, so we need to return our fake stack
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, &stack)
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
		}
		return nil
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
}

func TestCreateOrUpdateLokiStack_WhenGetReturnsNoError_UpdateObjects(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
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
					APIVersion:         "loki.grafana.com/v1",
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
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
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

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)
	require.NoError(t, err)

	// make sure create not called
	require.Zero(t, k.CreateCallCount())

	// make sure update was called
	require.NotZero(t, k.UpdateCallCount())
}

func TestCreateOrUpdateLokiStack_WhenCreateReturnsError_ContinueWithOtherObjects(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "someStack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
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

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)

	// make sure error is returned to re-trigger reconciliation
	require.Error(t, err)
}

func TestCreateOrUpdateLokiStack_WhenUpdateReturnsError_ContinueWithOtherObjects(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
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
					APIVersion:         "loki.grafana.com/v1",
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
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
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

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)

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
		Reason:  lokiv1.ReasonMissingObjectStorageSecret,
		Requeue: false,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, stack)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)

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
		Reason:  lokiv1.ReasonInvalidObjectStorageSecret,
		Requeue: false,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: invalidSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
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

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestCreateOrUpdateLokiStack_WithInvalidStorageSchema_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid object storage schema contents: spec does not contain any schemas",
		Reason:  lokiv1.ReasonInvalidObjectStorageSchema,
		Requeue: false,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
		},
		Status: lokiv1.LokiStackStatus{
			Storage: lokiv1.LokiStackStorageStatus{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
					{
						Version:       lokiv1.ObjectStorageSchemaV12,
						EffectiveDate: "2021-10-11",
					},
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, stack)
			return nil
		}
		if name.Name == defaultSecret.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestCreateOrUpdateLokiStack_WhenMissingCAConfigMap_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Missing object storage CA config map",
		Reason:  lokiv1.ReasonMissingObjectStorageCAConfigMap,
		Requeue: false,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
				TLS: &lokiv1.ObjectStorageTLSSpec{
					CA: "not-existing",
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, stack)
			return nil
		}

		if name.Name == defaultSecret.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}

		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestCreateOrUpdateLokiStack_WhenInvalidCAConfigMap_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: "Invalid object storage CA configmap contents: missing key or no contents",
		Reason:  lokiv1.ReasonInvalidObjectStorageCAConfigMap,
		Requeue: false,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
				TLS: &lokiv1.ObjectStorageTLSSpec{
					CA: invalidCAConfigMap.Name,
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, stack)
			return nil
		}
		if name.Name == defaultSecret.Name {
			k.SetClientObject(object, &defaultSecret)
			return nil
		}

		if name.Name == invalidCAConfigMap.Name {
			k.SetClientObject(object, &invalidCAConfigMap)
			return nil
		}
		return apierrors.NewNotFound(schema.GroupResource{}, "something is not found")
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)

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
		Reason:  lokiv1.ReasonInvalidTenantsConfiguration,
		Requeue: false,
	}

	ff := configv1.FeatureGates{
		LokiStackGateway: true,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1.OIDCSpec{
							Secret: &lokiv1.TenantSecretSpec{
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
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
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
		Reason:  lokiv1.ReasonMissingGatewayTenantSecret,
		Requeue: true,
	}

	ff := configv1.FeatureGates{
		LokiStackGateway: true,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1.OIDCSpec{
							Secret: &lokiv1.TenantSecretSpec{
								Name: defaultGatewaySecret.Name,
							},
						},
					},
				},
				Authorization: &lokiv1.AuthorizationSpec{
					OPA: &lokiv1.OPASpec{
						URL: "some-url",
					},
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		o, ok := object.(*lokiv1.LokiStack)
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
		Reason:  lokiv1.ReasonInvalidGatewayTenantSecret,
		Requeue: true,
	}

	ff := configv1.FeatureGates{
		LokiStackGateway: true,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1.OIDCSpec{
							Secret: &lokiv1.TenantSecretSpec{
								Name: invalidSecret.Name,
							},
						},
					},
				},
				Authorization: &lokiv1.AuthorizationSpec{
					OPA: &lokiv1.OPASpec{
						URL: "some-url",
					},
				},
			},
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		o, ok := object.(*lokiv1.LokiStack)
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
		Reason:  lokiv1.ReasonInvalidTenantsConfiguration,
		Requeue: false,
	}

	ff := configv1.FeatureGates{
		LokiStackGateway: true,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
			Tenants: nil,
		},
	}

	// GetStub looks up the CR first, so we need to return our fake stack
	// return NotFound for everything else to trigger create.
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		o, ok := object.(*lokiv1.LokiStack)
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

func TestCreateOrUpdateLokiStack_WhenInvalidQueryTimeout_SetDegraded(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	degradedErr := &status.DegradedError{
		Message: `Error parsing query timeout: time: invalid duration "invalid"`,
		Reason:  lokiv1.ReasonQueryTimeoutInvalid,
		Requeue: false,
	}

	stack := &lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV12,
						EffectiveDate: "2023-05-22",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: "openshift",
			},
			Limits: &lokiv1.LimitsSpec{
				Global: &lokiv1.LimitsTemplateSpec{
					QueryLimits: &lokiv1.QueryLimitSpec{
						QueryTimeout: "invalid",
					},
				},
			},
		},
	}

	// Create looks up the CR first, so we need to return our fake stack
	k.GetStub = func(_ context.Context, name types.NamespacedName, object client.Object, _ ...client.GetOption) error {
		if r.Name == name.Name && r.Namespace == name.Namespace {
			k.SetClientObject(object, stack)
		}
		if defaultSecret.Name == name.Name {
			k.SetClientObject(object, &defaultSecret)
		}
		return nil
	}

	k.StatusStub = func() client.StatusWriter { return sw }

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)

	// make sure error is returned
	require.Error(t, err)
	require.Equal(t, degradedErr, err)
}

func TestCreateOrUpdateLokiStack_RemovesRulerResourcesWhenDisabled(t *testing.T) {
	sw := &k8sfakes.FakeStatusWriter{}
	k := &k8sfakes.FakeClient{}
	r := ctrl.Request{
		NamespacedName: types.NamespacedName{
			Name:      "my-stack",
			Namespace: "some-ns",
		},
	}

	stack := lokiv1.LokiStack{
		TypeMeta: metav1.TypeMeta{
			Kind: "LokiStack",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "my-stack",
			Namespace: "some-ns",
			UID:       "b23f9a38-9672-499f-8c29-15ede74d3ece",
		},
		Spec: lokiv1.LokiStackSpec{
			Size: lokiv1.SizeOneXExtraSmall,
			Storage: lokiv1.ObjectStorageSpec{
				Schemas: []lokiv1.ObjectStorageSchema{
					{
						Version:       lokiv1.ObjectStorageSchemaV11,
						EffectiveDate: "2020-10-11",
					},
				},
				Secret: lokiv1.ObjectStorageSecretSpec{
					Name: defaultSecret.Name,
					Type: lokiv1.ObjectStorageSecretS3,
				},
			},
			Rules: &lokiv1.RulesSpec{
				Enabled: true,
			},
			Tenants: &lokiv1.TenantsSpec{
				Mode: "dynamic",
				Authentication: []lokiv1.AuthenticationSpec{
					{
						TenantName: "test",
						TenantID:   "1234",
						OIDC: &lokiv1.OIDCSpec{
							Secret: &lokiv1.TenantSecretSpec{
								Name: defaultGatewaySecret.Name,
							},
						},
					},
				},
				Authorization: &lokiv1.AuthorizationSpec{
					OPA: &lokiv1.OPASpec{
						URL: "some-url",
					},
				},
			},
		},
	}

	k.GetStub = func(_ context.Context, name types.NamespacedName, out client.Object, _ ...client.GetOption) error {
		_, ok := out.(*lokiv1.RulerConfig)
		if ok {
			return apierrors.NewNotFound(schema.GroupResource{}, "no ruler config")
		}
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

	k.StatusStub = func() client.StatusWriter { return sw }

	k.DeleteStub = func(_ context.Context, o client.Object, _ ...client.DeleteOption) error {
		assert.Equal(t, r.Namespace, o.GetNamespace())
		return nil
	}

	k.ListStub = func(_ context.Context, list client.ObjectList, options ...client.ListOption) error {
		switch list.(type) {
		case *corev1.ConfigMapList:
			k.SetClientObjectList(list, &corev1.ConfigMapList{
				Items: []corev1.ConfigMap{
					rulesCM,
				},
			})
		}
		return nil
	}

	err := handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)
	require.NoError(t, err)

	// make sure create was called
	require.NotZero(t, k.CreateCallCount())

	// make sure delete not called
	require.Zero(t, k.DeleteCallCount())

	// Disable the ruler
	stack.Spec.Rules.Enabled = false

	// Get should return ruler resources
	k.GetStub = func(_ context.Context, name types.NamespacedName, out client.Object, _ ...client.GetOption) error {
		_, ok := out.(*lokiv1.RulerConfig)
		if ok {
			return apierrors.NewNotFound(schema.GroupResource{}, "no ruler config")
		}
		if rulesCM.Name == name.Name {
			k.SetClientObject(out, &rulesCM)
			return nil
		}
		if rulerSS.Name == name.Name {
			k.SetClientObject(out, &rulerSS)
			return nil
		}
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
	err = handlers.CreateOrUpdateLokiStack(context.TODO(), logger, r, k, scheme, featureGates)
	require.NoError(t, err)

	// make sure delete was called twice (delete rules configmap and ruler statefulset)
	require.Equal(t, 2, k.DeleteCallCount())
}
