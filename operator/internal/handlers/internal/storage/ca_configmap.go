package storage

import corev1 "k8s.io/api/core/v1"

// IsValidCAConfigMap checks if the given CA configMap has an
// non-empty entry for key `service-ca.crt`
func IsValidCAConfigMap(cm *corev1.ConfigMap) bool {
	return cm.Data["service-ca.crt"] != ""
}
