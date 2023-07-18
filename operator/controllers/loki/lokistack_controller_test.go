package controllers

import (
	"flag"
	"io"
	"os"
	"testing"

	"github.com/ViaQ/logerr/v2/log"
	"github.com/go-logr/logr"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"
	"github.com/grafana/loki/operator/internal/external/k8s/k8sfakes"
	openshiftconfigv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/source"
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
		obj   client.Object
		index int
		pred  builder.OwnsOption
	}
	table := []test{
		{
			obj:   &corev1.ConfigMap{},
			index: 0,
			pred:  updateOrDeleteOnlyPred,
		},
		{
			obj:   &corev1.Secret{},
			index: 1,
			pred:  updateOrDeleteOnlyPred,
		},
		{
			obj:   &corev1.ServiceAccount{},
			index: 2,
			pred:  updateOrDeleteOnlyPred,
		},
		{
			obj:   &corev1.Service{},
			index: 3,
			pred:  updateOrDeleteOnlyPred,
		},
		{
			obj:   &appsv1.Deployment{},
			index: 4,
			pred:  updateOrDeleteWithStatusPred,
		},
		{
			obj:   &appsv1.StatefulSet{},
			index: 5,
			pred:  updateOrDeleteWithStatusPred,
		},
		{
			obj:   &rbacv1.ClusterRole{},
			index: 6,
			pred:  updateOrDeleteOnlyPred,
		},
		{
			obj:   &rbacv1.ClusterRoleBinding{},
			index: 7,
			pred:  updateOrDeleteOnlyPred,
		},
		{
			obj:   &rbacv1.Role{},
			index: 8,
			pred:  updateOrDeleteOnlyPred,
		},
		{
			obj:   &rbacv1.RoleBinding{},
			index: 9,
			pred:  updateOrDeleteOnlyPred,
		},
		{
			obj:   &monitoringv1.PrometheusRule{},
			index: 10,
			pred:  updateOrDeleteOnlyPred,
		},
		{
			obj:   &routev1.Route{},
			index: 11,
			pred:  updateOrDeleteOnlyPred,
		},
		{
			obj:   &networkingv1.Ingress{},
			index: 12,
			pred:  updateOrDeleteOnlyPred,
		},
	}
	for _, tst := range table {
		b := &k8sfakes.FakeBuilder{}
		b.ForReturns(b)
		b.OwnsReturns(b)
		b.WatchesReturns(b)

		c := &LokiStackReconciler{Client: k, Scheme: scheme}
		err := c.buildController(b)
		require.NoError(t, err)

		// Require Owns-Calls for all owned resources
		require.Equal(t, 13, b.OwnsCallCount())

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
		index        int
		featureGates lokiv1beta1.FeatureGates
		src          source.Source
		pred         builder.OwnsOption
	}
	table := []test{
		{
			src:   &source.Kind{Type: &openshiftconfigv1.APIServer{}},
			index: 0,
			pred:  updateOrDeleteOnlyPred,
		},
		{
			src:   &source.Kind{Type: &openshiftconfigv1.Proxy{}},
			index: 1,
			pred:  updateOrDeleteOnlyPred,
		},
		{
			src:          &source.Kind{Type: &corev1.Service{}},
			index:        2,
			featureGates: lokiv1beta1.FeatureGates{},
			pred:         createUpdateOrDeletePred,
		},
		{
			src:          &source.Kind{Type: &corev1.Secret{}},
			index:        3,
			featureGates: lokiv1beta1.FeatureGates{},
			pred:         createUpdateOrDeletePred,
		},
	}
	for _, tst := range table {
		b := &k8sfakes.FakeBuilder{}
		b.ForReturns(b)
		b.OwnsReturns(b)
		b.WatchesReturns(b)

		c := &LokiStackReconciler{Client: k, Scheme: scheme}
		err := c.buildController(b)
		require.NoError(t, err)

		// Require Watches-calls for all watches resources
		require.Equal(t, 4, b.WatchesCallCount())

		src, _, opts := b.WatchesArgsForCall(tst.index)
		require.Equal(t, tst.src, src)
		require.Equal(t, tst.pred, opts[0])
	}
}
