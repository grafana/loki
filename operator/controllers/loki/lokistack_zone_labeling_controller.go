package controllers

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"

	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/handlers"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
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
	var lokiPod corev1.Pod
	if err := r.Client.Get(ctx, req.NamespacedName, &lokiPod); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, kverrors.Wrap(err, "failed to lookup lokistack", "name", req.NamespacedName)
	}

	labels := lokiPod.GetLabels()
	for key, value := range labels {
		if key == "loki.grafana.com/zoneaware" {
			err := handlers.AnnotatePodsWithNodeLabels(ctx, r.Log, r.Client, lokiPod, value)
			if err != nil {
				return ctrl.Result{}, err
			}
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
		Named("LokiPod").
		Watches(&source.Kind{Type: &corev1.Pod{}}, &handler.EnqueueRequestForObject{}, createOrUpdatePred).
		Complete(r)
}
