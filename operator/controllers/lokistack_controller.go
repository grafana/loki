package controllers

import (
	"context"
	"errors"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	"github.com/go-logr/logr"
	"github.com/grafana/loki/operator/controllers/internal/management/state"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/handlers"
	"github.com/grafana/loki/operator/internal/manifests"
	"github.com/grafana/loki/operator/internal/status"
	routev1 "github.com/openshift/api/route/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	lokiv1beta1 "github.com/grafana/loki/operator/api/v1beta1"
)

var (
	createOrUpdateOnlyPred = builder.WithPredicates(predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Update only if generation changes, filter out anything else.
			// We only need to check generation here, because it is only
			// updated on spec changes. On the other hand RevisionVersion
			// changes also on status changes. We want to omit reconciliation
			// for status updates for now.
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
		},
		CreateFunc:  func(e event.CreateEvent) bool { return true },
		DeleteFunc:  func(e event.DeleteEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	})
	updateOrDeleteOnlyPred = builder.WithPredicates(predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			switch e.ObjectOld.(type) {
			case *appsv1.Deployment:
			case *appsv1.StatefulSet:
				return true
			}
			return false
		},
		CreateFunc: func(e event.CreateEvent) bool { return false },
		DeleteFunc: func(e event.DeleteEvent) bool {
			// DeleteStateUnknown evaluates to false only if the object
			// has been confirmed as deleted by the api server.
			return !e.DeleteStateUnknown
		},
		GenericFunc: func(e event.GenericEvent) bool { return false },
	})
)

// LokiStackReconciler reconciles a LokiStack object
type LokiStackReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
	Flags  manifests.FeatureFlags
}

// +kubebuilder:rbac:groups=loki.grafana.com,resources=lokistacks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=loki.grafana.com,resources=lokistacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=loki.grafana.com,resources=lokistacks/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods;nodes;services;endpoints;configmaps;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings;clusterroles;roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;prometheusrules,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;create;update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=config.openshift.io,resources=dnses,verbs=get;list;watch
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// Compare the state specified by the LokiStack object against the actual cluster state,
// and then perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.0/pkg/reconcile
func (r *LokiStackReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	ok, err := state.IsManaged(ctx, req, r.Client)
	if err != nil {
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: time.Second,
		}, err
	}
	if !ok {
		r.Log.Info("Skipping reconciliation for unmanaged lokistack resource", "name", req.NamespacedName)
		// Stop requeueing for unmanaged LokiStack custom resources
		return ctrl.Result{}, nil
	}

	err = handlers.CreateOrUpdateLokiStack(ctx, r.Log, req, r.Client, r.Scheme, r.Flags)

	var degraded *status.DegradedError
	if errors.As(err, &degraded) {
		err = status.SetDegradedCondition(ctx, r.Client, req, degraded.Message, degraded.Reason)
		if err != nil {
			return ctrl.Result{
				Requeue:      true,
				RequeueAfter: time.Second,
			}, err
		}

		return ctrl.Result{
			Requeue:      degraded.Requeue,
			RequeueAfter: time.Second,
		}, nil
	}

	if err != nil {
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: time.Second,
		}, err
	}

	err = status.Refresh(ctx, r.Client, req)
	if err != nil {
		return ctrl.Result{
			Requeue:      true,
			RequeueAfter: time.Second,
		}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *LokiStackReconciler) SetupWithManager(mgr manager.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr)
	return r.buildController(k8s.NewCtrlBuilder(b))
}

func (r *LokiStackReconciler) buildController(bld k8s.Builder) error {
	bld = bld.
		For(&lokiv1beta1.LokiStack{}, createOrUpdateOnlyPred).
		Owns(&corev1.ConfigMap{}, updateOrDeleteOnlyPred).
		Owns(&corev1.ServiceAccount{}, updateOrDeleteOnlyPred).
		Owns(&corev1.Service{}, updateOrDeleteOnlyPred).
		Owns(&appsv1.Deployment{}, updateOrDeleteOnlyPred).
		Owns(&appsv1.StatefulSet{}, updateOrDeleteOnlyPred).
		Owns(&rbacv1.ClusterRole{}, updateOrDeleteOnlyPred).
		Owns(&rbacv1.ClusterRoleBinding{}, updateOrDeleteOnlyPred).
		Owns(&rbacv1.Role{}, updateOrDeleteOnlyPred).
		Owns(&rbacv1.RoleBinding{}, updateOrDeleteOnlyPred)

	if r.Flags.EnablePrometheusAlerts {
		bld = bld.Owns(&monitoringv1.PrometheusRule{}, updateOrDeleteOnlyPred)
	}

	if r.Flags.EnableGatewayRoute {
		bld = bld.Owns(&routev1.Route{}, updateOrDeleteOnlyPred)
	} else {
		bld = bld.Owns(&networkingv1.Ingress{}, updateOrDeleteOnlyPred)
	}

	return bld.Complete(r)
}
