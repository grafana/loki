package controllers

import (
	"context"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"k8s.io/apimachinery/pkg/labels"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var deletesOnlyPred = builder.WithPredicates(predicate.Funcs{
	UpdateFunc:  func(e event.UpdateEvent) bool { return false },
	CreateFunc:  func(e event.CreateEvent) bool { return false },
	DeleteFunc:  func(e event.DeleteEvent) bool { return true },
	GenericFunc: func(e event.GenericEvent) bool { return false },
})

// LokiStackDasboardsReconciler cleans up all remaining
// dashboards when all LokiStack objects are removed.
type LokiStackDasboardsReconciler struct {
	client.Client
	Log logr.Logger
}

// Reconcile removes all LokiStack dashboard ConfigMap objects on OpenShift clusters when
// the last LokiStack custom resource is deleted.
func (r *LokiStackDasboardsReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	var stacks lokiv1.LokiStackList
	err := r.List(ctx, &stacks, client.MatchingLabelsSelector{Selector: labels.Everything()})
	if err != nil {
		return ctrl.Result{}, kverrors.Wrap(err, "failed to list any lokistack instances", "req")
	}

	if len(stacks.Items) > 0 {
		return ctrl.Result{}, nil
	}

	objs, err := openshift.BuildDashboards(openshift.Options{
		BuildOpts: openshift.BuildOptions{
			LokiStackName:      req.Name,
			LokiStackNamespace: req.Namespace,
		},
	})
	if err != nil {
		return ctrl.Result{}, kverrors.Wrap(err, "failed to build dashboards manifests", "req", req)
	}

	for _, obj := range objs {
		// Skip objects managed by owner references, e.g. PrometheusRules
		var skip bool
		for _, ref := range obj.GetOwnerReferences() {
			if ref.Kind == "LokiStack" && ref.Name == req.Name {
				skip = true
				break
			}
		}

		if skip {
			continue
		}

		key := client.ObjectKeyFromObject(obj)
		if err := r.Delete(ctx, obj, &client.DeleteOptions{}); err != nil {
			return ctrl.Result{}, kverrors.Wrap(err, "failed to delete dashboard", "key", key)
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LokiStackDasboardsReconciler) SetupWithManager(mgr manager.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&lokiv1.LokiStack{}, deletesOnlyPred).
		Complete(r)
}
