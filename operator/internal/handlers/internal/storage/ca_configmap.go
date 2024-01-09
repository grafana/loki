package storage

import (
	"crypto/sha1"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// CheckCAConfigMap checks if the given CA configMap has an non-empty entry for the key used as CA certificate.
// If the key is present it will return a hash of the current key name and contents.
func CheckCAConfigMap(cm *corev1.ConfigMap, key string) (string, error) {
	data := cm.Data[key]
	if data == "" {
		return "", fmt.Errorf("key not present or data empty: %s", key)
	}

	h := sha1.New()
	if _, err := h.Write([]byte(key)); err != nil {
		return "", err
	}

	if _, err := h.Write(hashSeparator); err != nil {
		return "", err
	}

	if _, err := h.Write([]byte(data)); err != nil {
		return "", err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
