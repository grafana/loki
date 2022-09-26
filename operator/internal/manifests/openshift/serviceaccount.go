package openshift

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildGatewayServiceAccount returns a k8s object for the LokiStack Gateway
// serviceaccount. This ServiceAccount is used in parallel as an
// OpenShift OAuth Client.
func BuildGatewayServiceAccount(opts Options) client.Object {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: serviceAccountAnnotations(opts),
			Labels:      opts.BuildOpts.Labels,
			Name:        gatewayServiceAccountName(opts),
			Namespace:   opts.BuildOpts.LokiStackNamespace,
		},
		AutomountServiceAccountToken: pointer.Bool(true),
	}
}

// BuildRulerServiceAccount returns a k8s object for the LokiStack Ruler
// serviceaccount.
// This ServiceAccount is used to autheticate and access the alertmanager host.
func BuildRulerServiceAccount(opts Options) client.Object {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: serviceAccountAnnotations(opts),
			Labels:      opts.BuildOpts.Labels,
			Name:        rulerServiceAccountName(opts),
			Namespace:   opts.BuildOpts.LokiStackNamespace,
		},
		AutomountServiceAccountToken: pointer.Bool(true),
	}
}
