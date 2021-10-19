package openshift

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// BuildServiceCAConfigMap returns a k8s configmap for the LokiStack
// gateway serviceCA configmap. This configmap is used to configure
// the gateway to proxy server-side TLS encrypted requests to Loki.
func BuildServiceCAConfigMap(opts Options) *corev1.ConfigMap {
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
			Namespace: opts.BuildOpts.GatewayNamespace,
		},
	}
}
