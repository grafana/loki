package storage

import (
	"context"
	"crypto/sha1"
	"fmt"

	"github.com/ViaQ/logerr/v2/kverrors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/status"
)

const (
	defaultCAKey = "service-ca.crt"
)

type caKeyError string

func (e caKeyError) Error() string {
	return fmt.Sprintf("key not present or data empty: %s", string(e))
}

func getCAConfigMap(ctx context.Context, k k8s.Client, stack *lokiv1.LokiStack, name string) (*corev1.ConfigMap, error) {
	var cm corev1.ConfigMap
	key := client.ObjectKey{Name: name, Namespace: stack.Namespace}
	if err := k.Get(ctx, key, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &status.DegradedError{
				Message: "Missing object storage CA config map",
				Reason:  lokiv1.ReasonMissingObjectStorageCAConfigMap,
				Requeue: false,
			}
		}
		return nil, kverrors.Wrap(err, "failed to lookup lokistack object storage CA config map", "name", key)
	}

	return &cm, nil
}

// checkCAConfigMap checks if the given CA configMap has an non-empty entry for the key used as CA certificate.
// If the key is present it will return a hash of the current key name and contents.
func checkCAConfigMap(cm *corev1.ConfigMap, key string) (string, error) {
	data := cm.Data[key]
	if data == "" {
		return "", caKeyError(key)
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
