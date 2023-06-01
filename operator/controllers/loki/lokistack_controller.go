package controllers

import (
	"context"
	"errors"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	"github.com/google/go-cmp/cmp"
	openshiftconfigv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/grafana/loki/operator/controllers/loki/internal/management/state"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/handlers"
	"github.com/grafana/loki/operator/internal/manifests/openshift"
	"github.com/grafana/loki/operator/internal/status"

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

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
)

var (
	createOrUpdateOnlyPred = builder.WithPredicates(predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			// Update only if generation or annotations change, filter out anything else.
			// We only need to check generation or annotations change here, because it is only
			// updated on spec changes. On the other hand RevisionVersion
			// changes also on status changes. We want to omit reconciliation
			// for status updates for now.
			return (e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()) ||
				cmp.Diff(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations()) != ""
		},
		CreateFunc:  func(e event.CreateEvent) bool { return true },
		DeleteFunc:  func(e event.DeleteEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	})
	updateOrDeleteOnlyPred = builder.WithPredicates(predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			switch e.ObjectOld.(type) {
			case *openshiftconfigv1.Proxy:
				// Update on any change of the RevisionVersion to capture
				// status updates for the proxy object. On OpenShift the
				// proxy status indicates that values for httpProxy/httpsProxy/noProxy
				// are valid after considering the readiness probes to access
				// the public net through these proxies.
				return true
			default:
				// Update only if generation change, filter out anything else.
				// We only need to check generation change here, because it is only
				// updated on spec changes. On the other hand RevisionVersion
				// changes also on status changes. We want to omit reconciliation
				// for status updates for now.
				return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
			}
		},
		CreateFunc: func(e event.CreateEvent) bool { return false },
		DeleteFunc: func(e event.DeleteEvent) bool {
			// DeleteStateUnknown evaluates to false only if the object
			// has been confirmed as deleted by the api server.
			return !e.DeleteStateUnknown
		},
		GenericFunc: func(e event.GenericEvent) bool { return false },
	})
	updateOrDeleteWithStatusPred = builder.WithPredicates(predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() || statusDifferent(e)
		},
		CreateFunc: func(_ event.CreateEvent) bool {
			return false
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// DeleteStateUnknown evaluates to false only if the object
			// has been confirmed as deleted by the api server.
			return !e.DeleteStateUnknown
		},
		GenericFunc: func(_ event.GenericEvent) bool {
			return false
		},
	})
	createUpdateOrDeletePred = builder.WithPredicates(predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			if e.ObjectOld.GetGeneration() == 0 && len(e.ObjectOld.GetAnnotations()) == 0 {
				return e.ObjectOld.GetResourceVersion() != e.ObjectNew.GetResourceVersion()
			}

			return e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration() ||
				cmp.Diff(e.ObjectOld.GetAnnotations(), e.ObjectNew.GetAnnotations()) != ""
		},
		CreateFunc:  func(e event.CreateEvent) bool { return true },
		DeleteFunc:  func(e event.DeleteEvent) bool { return true },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	})
)

// LokiStackReconciler reconciles a LokiStack object
type LokiStackReconciler struct {
	client.Client
	Log          logr.Logger
	Scheme       *runtime.Scheme
	FeatureGates configv1.FeatureGates
}

// +kubebuilder:rbac:groups=loki.grafana.com,resources=lokistacks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=loki.grafana.com,resources=lokistacks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=loki.grafana.com,resources=lokistacks/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=pods;nodes;services;endpoints;configmaps;secrets;serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps,resources=deployments;statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings;clusterroles;roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;prometheusrules,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=alertmanagers,verbs=patch
// +kubebuilder:rbac:urls=/api/v2/alerts,verbs=create
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;create;update
// +kubebuilder:rbac:groups=networking.k8s.io,resources=ingresses,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=policy,resources=poddisruptionbudgets,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=config.openshift.io,resources=dnses;apiservers;proxies,verbs=get;list;watch
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=get;list;watch;create;update;delete

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
		return ctrl.Result{}, err
	}
	if !ok {
		r.Log.Info("Skipping reconciliation for unmanaged lokistack resource", "name", req.NamespacedName)
		// Stop requeueing for unmanaged LokiStack custom resources
		return ctrl.Result{}, nil
	}

	if r.FeatureGates.BuiltInCertManagement.Enabled {
		err = handlers.CreateOrRotateCertificates(ctx, r.Log, req, r.Client, r.Scheme, r.FeatureGates)
		if err != nil {
			return handleDegradedError(ctx, r.Client, req, err)
		}
	}

	err = handlers.CreateOrUpdateLokiStack(ctx, r.Log, req, r.Client, r.Scheme, r.FeatureGates)
	if err != nil {
		return handleDegradedError(ctx, r.Client, req, err)
	}

	err = status.Refresh(ctx, r.Client, req, time.Now())
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func handleDegradedError(ctx context.Context, c client.Client, req ctrl.Request, err error) (ctrl.Result, error) {
	var degraded *status.DegradedError
	if errors.As(err, &degraded) {
		err = status.SetDegradedCondition(ctx, c, req, degraded.Message, degraded.Reason)
		if err != nil {
			return ctrl.Result{}, kverrors.Wrap(err, "error setting degraded condition")
		}

		return ctrl.Result{
			Requeue: degraded.Requeue,
		}, nil
	}

	return ctrl.Result{}, err
}

// SetupWithManager sets up the controller with the Manager.
func (r *LokiStackReconciler) SetupWithManager(mgr manager.Manager) error {
	b := ctrl.NewControllerManagedBy(mgr)
	return r.buildController(k8s.NewCtrlBuilder(b))
}

func (r *LokiStackReconciler) buildController(bld k8s.Builder) error {
	bld = bld.
		For(&lokiv1.LokiStack{}, createOrUpdateOnlyPred).
		Owns(&corev1.ConfigMap{}, updateOrDeleteOnlyPred).
		Owns(&corev1.Secret{}, updateOrDeleteOnlyPred).
		Owns(&corev1.ServiceAccount{}, updateOrDeleteOnlyPred).
		Owns(&corev1.Service{}, updateOrDeleteOnlyPred).
		Owns(&appsv1.Deployment{}, updateOrDeleteWithStatusPred).
		Owns(&appsv1.StatefulSet{}, updateOrDeleteWithStatusPred).
		Owns(&rbacv1.ClusterRole{}, updateOrDeleteOnlyPred).
		Owns(&rbacv1.ClusterRoleBinding{}, updateOrDeleteOnlyPred).
		Owns(&rbacv1.Role{}, updateOrDeleteOnlyPred).
		Owns(&rbacv1.RoleBinding{}, updateOrDeleteOnlyPred).
		Watches(&source.Kind{Type: &corev1.Service{}}, r.enqueueForAlertManagerServices(), createUpdateOrDeletePred).
		Watches(&source.Kind{Type: &corev1.Secret{}}, r.enqueueForStorageSecret(), createUpdateOrDeletePred)

	if r.FeatureGates.LokiStackAlerts {
		bld = bld.Owns(&monitoringv1.PrometheusRule{}, updateOrDeleteOnlyPred)
	}

	if r.FeatureGates.OpenShift.Enabled {
		bld = bld.Owns(&routev1.Route{}, updateOrDeleteOnlyPred)
	} else {
		bld = bld.Owns(&networkingv1.Ingress{}, updateOrDeleteOnlyPred)
	}

	if r.FeatureGates.OpenShift.ClusterTLSPolicy {
		bld = bld.Watches(&source.Kind{Type: &openshiftconfigv1.APIServer{}}, r.enqueueAllLokiStacksHandler(), updateOrDeleteOnlyPred)
	}

	if r.FeatureGates.OpenShift.ClusterProxy {
		bld = bld.Watches(&source.Kind{Type: &openshiftconfigv1.Proxy{}}, r.enqueueAllLokiStacksHandler(), updateOrDeleteOnlyPred)
	}

	return bld.Complete(r)
}

func (r *LokiStackReconciler) enqueueAllLokiStacksHandler() handler.EventHandler {
	ctx := context.TODO()
	return handler.EnqueueRequestsFromMapFunc(func(obj client.Object) []reconcile.Request {
		lokiStacks := &lokiv1.LokiStackList{}
		if err := r.Client.List(ctx, lokiStacks); err != nil {
			r.Log.Error(err, "Error getting LokiStack resources in event handler")
			return nil
		}

		var requests []reconcile.Request
		for _, stack := range lokiStacks.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: stack.Namespace,
					Name:      stack.Name,
				},
			})
		}

		r.Log.Info("Enqueued requests for all LokiStacks because of global resource change", "count", len(requests), "kind", obj.GetObjectKind())
		return requests
	})
}

func statusDifferent(e event.UpdateEvent) bool {
	switch old := e.ObjectOld.(type) {
	case *appsv1.Deployment:
		newObject := e.ObjectNew.(*appsv1.Deployment)
		return cmp.Diff(old.Status, newObject.Status) != ""
	case *appsv1.StatefulSet:
		newObject := e.ObjectNew.(*appsv1.StatefulSet)
		return cmp.Diff(old.Status, newObject.Status) != ""
	default:
		return false
	}
}

func (r *LokiStackReconciler) enqueueForAlertManagerServices() handler.EventHandler {
	ctx := context.TODO()
	return handler.EnqueueRequestsFromMapFunc(func(obj client.Object) []reconcile.Request {
		lokiStacks := &lokiv1.LokiStackList{}
		if err := r.Client.List(ctx, lokiStacks); err != nil {
			r.Log.Error(err, "Error getting LokiStack resources in event handler")
			return nil
		}
		var requests []reconcile.Request

		if obj.GetName() == openshift.MonitoringSVCOperated &&
			(obj.GetNamespace() == openshift.MonitoringUserwWrkloadNS ||
				obj.GetNamespace() == openshift.MonitoringNS) {

			for _, stack := range lokiStacks.Items {
				if stack.Spec.Tenants != nil && (stack.Spec.Tenants.Mode == lokiv1.OpenshiftLogging ||
					stack.Spec.Tenants.Mode == lokiv1.OpenshiftNetwork) {
					requests = append(requests, reconcile.Request{
						NamespacedName: types.NamespacedName{
							Namespace: stack.Namespace,
							Name:      stack.Name,
						},
					})
				}
			}

			r.Log.Info("Enqueued requests for all LokiStacks because of UserWorkload Alertmanager Service resource change", "count", len(requests), "kind", obj.GetObjectKind())
		}

		return requests
	})
}

func (r *LokiStackReconciler) enqueueForStorageSecret() handler.EventHandler {
	ctx := context.TODO()
	return handler.EnqueueRequestsFromMapFunc(func(obj client.Object) []reconcile.Request {
		lokiStacks := &lokiv1.LokiStackList{}
		if err := r.Client.List(ctx, lokiStacks); err != nil {
			r.Log.Error(err, "Error getting LokiStack resources in event handler")
			return nil
		}

		var requests []reconcile.Request
		for _, stack := range lokiStacks.Items {
			if obj.GetName() == stack.Spec.Storage.Secret.Name && obj.GetNamespace() == stack.Namespace {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: stack.Namespace,
						Name:      stack.Name,
					},
				})
				r.Log.Info("Enqueued requests for LokiStack because of Storage Secret resource change", "LokiStack", stack.Name, "Secret", obj.GetName())

				return requests
			}
		}

		return requests
	})
}
