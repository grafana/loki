package state

import (
	"context"
	"sync"

	"github.com/go-logr/logr"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"

	"github.com/ViaQ/logerr/v2/kverrors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// IsManaged checks if the custom resource is configured with ManagementState Managed.
func IsManaged(ctx context.Context, req ctrl.Request, k k8s.Client, log logr.Logger) (bool, error) {
	var stack lokiv1.LokiStack
	if err := k.Get(ctx, req.NamespacedName, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			unregisterLokiStackNamespacedName(log, req)
			return false, nil
		}
		return false, kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	return stack.Spec.ManagementState == lokiv1.ManagementStateManaged, nil
}

type RegisteredNamespacedNames struct {
	registered []types.NamespacedName
	mux        sync.Mutex
}

var registeredLokiStacks RegisteredNamespacedNames

// this is used for when we get an apiserver change
// it will return requests for all known loki CRs
func GetLokiStackEvents(a client.Object) []reconcile.Request {
	requests := []reconcile.Request{}

	registeredLokiStacks.mux.Lock()
	defer registeredLokiStacks.mux.Unlock()

	for _, loki := range registeredLokiStacks.registered {
		requests = append(requests, reconcile.Request{NamespacedName: loki})
	}

	return requests
}

func RegisterLokiStackNamespacedName(log logr.Logger, request reconcile.Request) {
	// check to see if the namespaced name is already registered first
	found := false

	registeredLokiStacks.mux.Lock()
	defer registeredLokiStacks.mux.Unlock()

	for _, loki := range registeredLokiStacks.registered {
		if loki == request.NamespacedName {
			found = true
		}
	}

	// if not, add it to registeredLokiStacks
	if !found {
		log.Info("Registering future events", "name", request.NamespacedName)
		registeredLokiStacks.registered = append(registeredLokiStacks.registered, request.NamespacedName)
	}
}

func unregisterLokiStackNamespacedName(log logr.Logger, request reconcile.Request) {
	// look for a namespacedname registered already
	found := false
	index := -1

	registeredLokiStacks.mux.Lock()
	defer registeredLokiStacks.mux.Unlock()

	for i, loki := range registeredLokiStacks.registered {
		if loki == request.NamespacedName {
			found = true
			index = i
		}
	}

	// if we find it, remove it from registeredLokiStacks
	if found {
		log.Info("Unregistering future events", "name", request.NamespacedName)
		registeredLokiStacks.registered = append(registeredLokiStacks.registered[:index], registeredLokiStacks.registered[index+1:]...)
	}
}
