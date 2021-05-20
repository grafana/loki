package controllers

import (
	"flag"
	"io/ioutil"
	"os"
	"testing"

	"github.com/ViaQ/logerr/log"
	lokiv1beta1 "github.com/ViaQ/loki-operator/api/v1beta1"
	"github.com/ViaQ/loki-operator/internal/external/k8s/k8sfakes"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
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

func TestLokiStackController_RegistersCustomResourceForCreateOrUpdate(t *testing.T) {
	b := &k8sfakes.FakeBuilder{}
	k := &k8sfakes.FakeClient{}
	c := &LokiStackReconciler{Client: k, Scheme: scheme}

	b.ForReturns(b)
	b.OwnsReturns(b)

	err := c.buildController(b)
	require.NoError(t, err)

	// Require only one For-Call for the custom resource
	require.Equal(t, 1, b.ForCallCount())

	// Require For-call options to have create and  update predicates
	obj, opts := b.ForArgsForCall(0)
	require.Equal(t, &lokiv1beta1.LokiStack{}, obj)
	require.Equal(t, opts[0], createOrUpdateOnlyPred)
}

func TestLokiStackController_RegisterOwnedResourcesForUpdateOrDeleteOnly(t *testing.T) {
	b := &k8sfakes.FakeBuilder{}
	k := &k8sfakes.FakeClient{}
	c := &LokiStackReconciler{Client: k, Scheme: scheme}

	b.ForReturns(b)
	b.OwnsReturns(b)

	err := c.buildController(b)
	require.NoError(t, err)

	// Require Owns-Calls for all owned resources
	require.Equal(t, 4, b.OwnsCallCount())

	// Require owned resources
	type test struct {
		obj  client.Object
		pred builder.OwnsOption
	}
	table := []test{
		{
			obj:  &corev1.ConfigMap{},
			pred: updateOrDeleteOnlyPred,
		},
		{
			obj:  &corev1.Service{},
			pred: updateOrDeleteOnlyPred,
		},
		{
			obj:  &appsv1.Deployment{},
			pred: updateOrDeleteOnlyPred,
		},
		{
			obj:  &appsv1.StatefulSet{},
			pred: updateOrDeleteOnlyPred,
		},
	}
	for i, tst := range table {
		// Require Owns-call options to have delete predicate only
		obj, opts := b.OwnsArgsForCall(i)
		require.Equal(t, tst.obj, obj)
		require.Equal(t, tst.pred, opts[0])
	}
}
