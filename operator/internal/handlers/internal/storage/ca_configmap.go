package storage

import corev1 "k8s.io/api/core/v1"

// isValidCAConfigMap checks if the given CA configMap has an
// non-empty entry for the key
func isValidCAConfigMap(cm *corev1.ConfigMap, key string) bool {
	return cm.Data[key] != ""
}
