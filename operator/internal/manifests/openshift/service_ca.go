package openshift

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildGatewayCAConfigMap returns a k8s configmap for the LokiStack
// serviceCA configmap. This configmap is used to configure
// the gateway and components to verify TLS certificates.
func BuildGatewayCAConfigMap(opts Options) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				InjectCABundleKey: "true",
			},
			Labels:    opts.BuildOpts.Labels,
			Name:      serviceCABundleName(opts),
			Namespace: opts.BuildOpts.LokiStackNamespace,
		},
	}
}

// BuildAlertManagerCAConfigMap returns a k8s configmap for the LokiStack
// alertmanager serviceCA configmap. This configmap is used to configure
// the ruler to verify AlertManager TLS certificates.
func BuildAlertManagerCAConfigMap(opts Options) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			Kind:       "ConfigMap",
			APIVersion: corev1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				InjectCABundleKey: "true",
			},
			Labels:    opts.BuildOpts.Labels,
			Name:      alertmanagerCABundleName(opts),
			Namespace: opts.BuildOpts.LokiStackNamespace,
		},
	}
}
