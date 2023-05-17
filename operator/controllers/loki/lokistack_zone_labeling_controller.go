package controllers

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/controllers/loki/internal/management/state"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/handlers"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var createOrUpdatePred = builder.WithPredicates(predicate.Funcs{
	UpdateFunc:  func(e event.UpdateEvent) bool { return true },
	CreateFunc:  func(e event.CreateEvent) bool { return true },
	DeleteFunc:  func(e event.DeleteEvent) bool { return false },
	GenericFunc: func(e event.GenericEvent) bool { return false },
})

// LokiStackZoneAwarePodReconciler watches all the loki component pods and updates the pod annotations with the topology node labels.
type LokiStackZoneAwarePodReconciler struct {
	client.Client
	Log logr.Logger
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.11.0/pkg/reconcile
func (r *LokiStackZoneAwarePodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	managed, err := state.IsManaged(ctx, req, r.Client)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !managed {
		r.Log.Info("Skipping reconciliation for unmanaged LokiStack resource", "name", req.String())
		// Stop requeueing for unmanaged LokiStack custom resources
		return ctrl.Result{}, nil
	}

	var stack lokiv1.LokiStack
	if err := r.Client.Get(ctx, req.NamespacedName, &stack); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	if len(stack.Spec.Replication.Zones) > 0 {
		err := handlers.AnnotatePodsWithNodeLabels(ctx, r.Log, r.Client, stack.Name, stack.Namespace, stack.Spec.Replication.Zones)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LokiStackZoneAwarePodReconciler) SetupWithManager(mgr ctrl.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr)
	return r.buildController(k8s.NewCtrlBuilder(b))
}

func (r *LokiStackZoneAwarePodReconciler) buildController(bld k8s.Builder) error {
	return bld.
		For(&lokiv1.LokiStack{}).
		Watches(&source.Kind{Type: &corev1.Pod{}}, r.enqueueForPodBinding(), createOrUpdatePred).
		Complete(r)
}

func (r *LokiStackZoneAwarePodReconciler) enqueueForPodBinding() handler.EventHandler {
	ctx := context.TODO()
	return handler.EnqueueRequestsFromMapFunc(func(obj client.Object) []reconcile.Request {
		lokiStacks := &lokiv1.LokiStackList{}
		if err := r.Client.List(ctx, lokiStacks); err != nil {
			r.Log.Error(err, "Error getting LokiStack resources in event handler")
			return nil
		}

		var requests []reconcile.Request

		for _, stack := range lokiStacks.Items {
			if obj.GetNamespace() == stack.Namespace && len(stack.Spec.Replication.Zones) > 0 {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: stack.Namespace,
						Name:      stack.Name,
					},
				})
				r.Log.Info("Enqueued requests for LokiStack because Zone Aware Pod was created or updated", "LokiStack", stack.Name, "Pod", obj.GetName())

				return requests
			}
		}

		return requests
	})
}
