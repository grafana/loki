package controllers

import (
	"flag"
	"io"
	"os"
	"testing"

	"github.com/ViaQ/logerr/v2/log"
	"github.com/go-logr/logr"
	openshiftconfigv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	cloudcredentialv1 "github.com/openshift/cloud-credential-operator/pkg/apis/cloudcredential/v1"
	"github.com/stretchr/testify/require"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
)

var (
	logger logr.Logger

	scheme = runtime.NewScheme()
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

func TestLokiStackController_RegistersCustomResourceForCreateOrUpdate(t *testing.T) {
	b := &k8sfakes.FakeBuilder{}
	k := &k8sfakes.FakeClient{}
	c := &LokiStackReconciler{Client: k, Scheme: scheme}

	b.ForReturns(b)
	b.OwnsReturns(b)
	b.WatchesReturns(b)

	err := c.buildController(b)
	require.NoError(t, err)

	// Require only one For-Call for the custom resource
	require.Equal(t, 1, b.ForCallCount())

	// Require For-call options to have create and  update predicates
	obj, opts := b.ForArgsForCall(0)
	require.Equal(t, &lokiv1.LokiStack{}, obj)
	require.Equal(t, opts[0], createOrUpdateOnlyPred)
}

func TestLokiStackController_RegisterOwnedResourcesForUpdateOrDeleteOnly(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	// Require owned resources
	type test struct {
		obj           client.Object
		index         int
		ownCallsCount int
		featureGates  configv1.FeatureGates
		pred          builder.OwnsOption
	}
	table := []test{
		{
			obj:           &corev1.ConfigMap{},
			index:         0,
			ownCallsCount: 11,
			pred:          updateOrDeleteOnlyPred,
		},
		{
			obj:           &corev1.Secret{},
			index:         1,
			ownCallsCount: 11,
			pred:          updateOrDeleteOnlyPred,
		},
		{
			obj:           &corev1.ServiceAccount{},
			index:         2,
			ownCallsCount: 11,
			pred:          updateOrDeleteOnlyPred,
		},
		{
			obj:           &corev1.Service{},
			index:         3,
			ownCallsCount: 11,
			pred:          updateOrDeleteOnlyPred,
		},
		{
			obj:           &appsv1.Deployment{},
			index:         4,
			ownCallsCount: 11,
			pred:          updateOrDeleteWithStatusPred,
		},
		{
			obj:           &appsv1.StatefulSet{},
			index:         5,
			ownCallsCount: 11,
			pred:          updateOrDeleteWithStatusPred,
		},
		{
			obj:           &rbacv1.ClusterRole{},
			index:         6,
			ownCallsCount: 11,
			pred:          updateOrDeleteOnlyPred,
		},
		{
			obj:           &rbacv1.ClusterRoleBinding{},
			index:         7,
			ownCallsCount: 11,
			pred:          updateOrDeleteOnlyPred,
		},
		{
			obj:           &rbacv1.Role{},
			index:         8,
			ownCallsCount: 11,
			pred:          updateOrDeleteOnlyPred,
		},
		{
			obj:           &rbacv1.RoleBinding{},
			index:         9,
			ownCallsCount: 11,
			pred:          updateOrDeleteOnlyPred,
		},
		// The next two share the same index, because the
		// controller either reconciles an Ingress (i.e. Kubernetes)
		// or a Route (i.e. OpenShift).
		{
			obj:           &networkingv1.Ingress{},
			index:         10,
			ownCallsCount: 11,
			featureGates: configv1.FeatureGates{
				OpenShift: configv1.OpenShiftFeatureGates{
					Enabled: false,
				},
			},
			pred: updateOrDeleteOnlyPred,
		},
		{
			obj:           &routev1.Route{},
			index:         10,
			ownCallsCount: 11,
			featureGates: configv1.FeatureGates{
				OpenShift: configv1.OpenShiftFeatureGates{
					Enabled: true,
				},
			},
			pred: updateOrDeleteOnlyPred,
		},
		{
			obj:           &cloudcredentialv1.CredentialsRequest{},
			index:         11,
			ownCallsCount: 12,
			featureGates: configv1.FeatureGates{
				OpenShift: configv1.OpenShiftFeatureGates{
					Enabled:         true,
					TokenCCOAuthEnv: true,
				},
			},
			pred: updateOrDeleteOnlyPred,
		},
	}
	for _, tst := range table {
		b := &k8sfakes.FakeBuilder{}
		b.ForReturns(b)
		b.OwnsReturns(b)
		b.WatchesReturns(b)

		c := &LokiStackReconciler{Client: k, Scheme: scheme, FeatureGates: tst.featureGates}
		err := c.buildController(b)
		require.NoError(t, err)

		// Require Owns-Calls for all owned resources
		require.Equal(t, tst.ownCallsCount, b.OwnsCallCount())

		// Require Owns-call options to have delete predicate only
		obj, opts := b.OwnsArgsForCall(tst.index)
		require.Equal(t, tst.obj, obj)
		require.Equal(t, tst.pred, opts[0])
	}
}

func TestLokiStackController_RegisterWatchedResources(t *testing.T) {
	k := &k8sfakes.FakeClient{}

	// Require owned resources
	type test struct {
		index             int
		watchesCallsCount int
		featureGates      configv1.FeatureGates
		src               client.Object
		pred              builder.OwnsOption
	}
	table := []test{
		{
			src:               &openshiftconfigv1.APIServer{},
			index:             3,
			watchesCallsCount: 4,
			featureGates: configv1.FeatureGates{
				OpenShift: configv1.OpenShiftFeatureGates{
					Enabled:          true,
					ClusterTLSPolicy: true,
				},
			},
			pred: updateOrDeleteOnlyPred,
		},
		{
			src:               &openshiftconfigv1.Proxy{},
			index:             3,
			watchesCallsCount: 4,
			featureGates: configv1.FeatureGates{
				OpenShift: configv1.OpenShiftFeatureGates{
					Enabled:      true,
					ClusterProxy: true,
				},
			},
			pred: updateOrDeleteOnlyPred,
		},
		{
			src:               &corev1.Service{},
			index:             0,
			watchesCallsCount: 3,
			featureGates:      configv1.FeatureGates{},
			pred:              createUpdateOrDeletePred,
		},
		{
			src:               &corev1.Secret{},
			index:             1,
			watchesCallsCount: 3,
			featureGates:      configv1.FeatureGates{},
			pred:              createUpdateOrDeletePred,
		},
		{
			src:               &corev1.ConfigMap{},
			index:             2,
			watchesCallsCount: 3,
			featureGates:      configv1.FeatureGates{},
			pred:              createUpdateOrDeletePred,
		},
	}
	for _, tst := range table {
		b := &k8sfakes.FakeBuilder{}
		b.ForReturns(b)
		b.OwnsReturns(b)
		b.WatchesReturns(b)

		c := &LokiStackReconciler{Client: k, Scheme: scheme, FeatureGates: tst.featureGates}
		err := c.buildController(b)
		require.NoError(t, err)

		// Require Watches-calls for all watches resources
		require.Equal(t, tst.watchesCallsCount, b.WatchesCallCount())

		src, _, opts := b.WatchesArgsForCall(tst.index)
		require.Equal(t, tst.src, src)
		require.Equal(t, tst.pred, opts[0])
	}
}
