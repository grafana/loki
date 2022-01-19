package openshift

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// BuildServiceAccount returns a k8s object for the LokiStack Gateway
// serviceaccount. This ServiceAccount is used in parallel as an
// OpenShift OAuth Client.
func BuildServiceAccount(opts Options) client.Object {
	return &corev1.ServiceAccount{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ServiceAccount",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: serviceAccountAnnotations(opts),
			Labels:      opts.BuildOpts.Labels,
			Name:        serviceAccountName(opts),
			Namespace:   opts.BuildOpts.GatewayNamespace,
		},
		AutomountServiceAccountToken: pointer.Bool(true),
	}
}
