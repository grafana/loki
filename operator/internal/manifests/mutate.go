package manifests

import (
	"reflect"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/imdario/mergo"
	routev1 "github.com/openshift/api/route/v1"
	cloudcredentialv1 "github.com/openshift/cloud-credential-operator/pkg/apis/cloudcredential/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	policyv1 "k8s.io/api/policy/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// MutateFuncFor returns a mutate function based on the existing resource's concrete type.
// It currently supports the following types and will return an error for other types:
//
//   - ConfigMap
//   - Secret
//   - Service
//   - ServiceAccount
//   - ClusterRole
//   - ClusterRoleBinding
//   - Role
//   - RoleBinding
//   - Deployment
//   - StatefulSet
//   - ServiceMonitor
//   - Ingress
//   - Route
//   - PrometheusRule
//   - PodDisruptionBudget
func MutateFuncFor(existing, desired client.Object, depAnnotations map[string]string) controllerutil.MutateFn {
	return func() error {
		existingAnnotations := existing.GetAnnotations()
		if err := mergeWithOverride(&existingAnnotations, desired.GetAnnotations()); err != nil {
			return err
		}
		existing.SetAnnotations(existingAnnotations)

		existingLabels := existing.GetLabels()
		if err := mergeWithOverride(&existingLabels, desired.GetLabels()); err != nil {
			return err
		}
		existing.SetLabels(existingLabels)

		if ownerRefs := desired.GetOwnerReferences(); len(ownerRefs) > 0 {
			existing.SetOwnerReferences(ownerRefs)
		}

		switch existing.(type) {
		case *corev1.ConfigMap:
			cm := existing.(*corev1.ConfigMap)
			wantCm := desired.(*corev1.ConfigMap)
			mutateConfigMap(cm, wantCm)

		case *corev1.Secret:
			s := existing.(*corev1.Secret)
			wantS := desired.(*corev1.Secret)
			mutateSecret(s, wantS)
			existingAnnotations := s.GetAnnotations()
			if err := mergeWithOverride(&existingAnnotations, depAnnotations); err != nil {
				return err
			}
			s.SetAnnotations(existingAnnotations)

		case *corev1.Service:
			svc := existing.(*corev1.Service)
			wantSvc := desired.(*corev1.Service)
			mutateService(svc, wantSvc)

		case *corev1.ServiceAccount:
			sa := existing.(*corev1.ServiceAccount)
			wantSa := desired.(*corev1.ServiceAccount)
			mutateServiceAccount(sa, wantSa)

		case *rbacv1.ClusterRole:
			cr := existing.(*rbacv1.ClusterRole)
			wantCr := desired.(*rbacv1.ClusterRole)
			mutateClusterRole(cr, wantCr)

		case *rbacv1.ClusterRoleBinding:
			crb := existing.(*rbacv1.ClusterRoleBinding)
			wantCrb := desired.(*rbacv1.ClusterRoleBinding)
			mutateClusterRoleBinding(crb, wantCrb)

		case *rbacv1.Role:
			r := existing.(*rbacv1.Role)
			wantR := desired.(*rbacv1.Role)
			mutateRole(r, wantR)

		case *rbacv1.RoleBinding:
			rb := existing.(*rbacv1.RoleBinding)
			wantRb := desired.(*rbacv1.RoleBinding)
			mutateRoleBinding(rb, wantRb)

		case *appsv1.Deployment:
			dpl := existing.(*appsv1.Deployment)
			wantDpl := desired.(*appsv1.Deployment)
			mutateDeployment(dpl, wantDpl)

		case *appsv1.StatefulSet:
			sts := existing.(*appsv1.StatefulSet)
			wantSts := desired.(*appsv1.StatefulSet)
			mutateStatefulSet(sts, wantSts)

		case *monitoringv1.ServiceMonitor:
			svcMonitor := existing.(*monitoringv1.ServiceMonitor)
			wantSvcMonitor := desired.(*monitoringv1.ServiceMonitor)
			mutateServiceMonitor(svcMonitor, wantSvcMonitor)

		case *networkingv1.Ingress:
			ing := existing.(*networkingv1.Ingress)
			wantIng := desired.(*networkingv1.Ingress)
			mutateIngress(ing, wantIng)

		case *routev1.Route:
			rt := existing.(*routev1.Route)
			wantRt := desired.(*routev1.Route)
			mutateRoute(rt, wantRt)

		case *cloudcredentialv1.CredentialsRequest:
			cr := existing.(*cloudcredentialv1.CredentialsRequest)
			wantCr := desired.(*cloudcredentialv1.CredentialsRequest)
			mutateCredentialRequest(cr, wantCr)

		case *monitoringv1.PrometheusRule:
			pr := existing.(*monitoringv1.PrometheusRule)
			wantPr := desired.(*monitoringv1.PrometheusRule)
			mutatePrometheusRule(pr, wantPr)
		case *policyv1.PodDisruptionBudget:
			pdb := existing.(*policyv1.PodDisruptionBudget)
			wantPdb := desired.(*policyv1.PodDisruptionBudget)
			mutatePodDisruptionBudget(pdb, wantPdb)

		default:
			t := reflect.TypeOf(existing).String()
			return kverrors.New("missing mutate implementation for resource type", "type", t)
		}
		return nil
	}
}

func mergeWithOverride(dst, src interface{}) error {
	err := mergo.Merge(dst, src, mergo.WithOverride)
	if err != nil {
		return kverrors.Wrap(err, "unable to mergeWithOverride", "dst", dst, "src", src)
	}
	return nil
}

func mutateConfigMap(existing, desired *corev1.ConfigMap) {
	existing.Annotations = desired.Annotations
	existing.Labels = desired.Labels
	existing.BinaryData = desired.BinaryData
	existing.Data = desired.Data
}

func mutateSecret(existing, desired *corev1.Secret) {
	existing.Annotations = desired.Annotations
	existing.Labels = desired.Labels
	existing.Data = desired.Data
}

func mutateServiceAccount(existing, desired *corev1.ServiceAccount) {
	existing.Labels = desired.Labels
}

func mutateClusterRole(existing, desired *rbacv1.ClusterRole) {
	existing.Annotations = desired.Annotations
	existing.Labels = desired.Labels
	existing.Rules = desired.Rules
}

func mutateClusterRoleBinding(existing, desired *rbacv1.ClusterRoleBinding) {
	existing.Annotations = desired.Annotations
	existing.Labels = desired.Labels
	existing.Subjects = desired.Subjects
}

func mutateRole(existing, desired *rbacv1.Role) {
	existing.Annotations = desired.Annotations
	existing.Labels = desired.Labels
	existing.Rules = desired.Rules
}

func mutateRoleBinding(existing, desired *rbacv1.RoleBinding) {
	existing.Annotations = desired.Annotations
	existing.Labels = desired.Labels
	existing.Subjects = desired.Subjects
}

func mutateServiceMonitor(existing, desired *monitoringv1.ServiceMonitor) {
	// ServiceMonitor selector is immutable so we set this value only if
	// a new object is going to be created
	existing.Annotations = desired.Annotations
	existing.Labels = desired.Labels
	existing.Spec.Endpoints = desired.Spec.Endpoints
	existing.Spec.JobLabel = desired.Spec.JobLabel
}

func mutateIngress(existing, desired *networkingv1.Ingress) {
	existing.Labels = desired.Labels
	existing.Spec.DefaultBackend = desired.Spec.DefaultBackend
	existing.Spec.Rules = desired.Spec.Rules
	existing.Spec.TLS = desired.Spec.TLS
}

func mutateRoute(existing, desired *routev1.Route) {
	existing.Labels = desired.Labels
	existing.Spec = desired.Spec
}

func mutateCredentialRequest(existing, desired *cloudcredentialv1.CredentialsRequest) {
	existing.Spec = desired.Spec
}

func mutatePrometheusRule(existing, desired *monitoringv1.PrometheusRule) {
	existing.Annotations = desired.Annotations
	existing.Labels = desired.Labels
	existing.Spec = desired.Spec
}

func mutateService(existing, desired *corev1.Service) {
	existing.Spec.Ports = desired.Spec.Ports
	existing.Spec.Selector = desired.Spec.Selector
}

func mutateDeployment(existing, desired *appsv1.Deployment) {
	// Deployment selector is immutable so we set this value only if
	// a new object is going to be created
	if existing.CreationTimestamp.IsZero() {
		existing.Spec.Selector = desired.Spec.Selector
	}
	existing.Spec.Replicas = desired.Spec.Replicas
	existing.Spec.Strategy = desired.Spec.Strategy
	mutatePodTemplate(&existing.Spec.Template, &desired.Spec.Template)
}

func mutateStatefulSet(existing, desired *appsv1.StatefulSet) {
	// StatefulSet selector is immutable so we set this value only if
	// a new object is going to be created
	if existing.CreationTimestamp.IsZero() {
		existing.Spec.Selector = desired.Spec.Selector
	}
	existing.Spec.Replicas = desired.Spec.Replicas
	mutatePodTemplate(&existing.Spec.Template, &desired.Spec.Template)
}

func mutatePodDisruptionBudget(existing, desired *policyv1.PodDisruptionBudget) {
	existing.Annotations = desired.Annotations
	existing.Labels = desired.Labels
	existing.Spec = desired.Spec
}

func mutatePodTemplate(existing, desired *corev1.PodTemplateSpec) {
	existing.Annotations = desired.Annotations
	existing.Labels = desired.Labels
	mutatePodSpec(&existing.Spec, &desired.Spec)
}

func mutatePodSpec(existing *corev1.PodSpec, desired *corev1.PodSpec) {
	existing.Affinity = desired.Affinity
	existing.Containers = desired.Containers
	existing.InitContainers = desired.InitContainers
	existing.NodeSelector = desired.NodeSelector
	existing.Tolerations = desired.Tolerations
	existing.TopologySpreadConstraints = desired.TopologySpreadConstraints
	existing.Volumes = desired.Volumes
}
