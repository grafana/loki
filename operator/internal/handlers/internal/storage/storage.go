package storage

import (
	"context"
	"fmt"
	"time"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/storage"
	"github.com/grafana/loki/operator/internal/status"
)

// BuildOptions returns the object storage options to generate Kubernetes resource manifests
// which require access to object storage buckets.
// The returned error can be a status.DegradedError in the following cases:
//   - The user-provided object storage secret is missing.
//   - The object storage Secret data is invalid.
//   - The object storage schema config is invalid.
//   - The object storage CA ConfigMap is missing if one referenced.
//   - The object storage CA ConfigMap data is invalid.
//   - The object storage managed auth secret is missing (Only on OpenShift STS-clusters)
func BuildOptions(ctx context.Context, k k8s.Client, stack *lokiv1.LokiStack, fg configv1.FeatureGates) (storage.Options, error) {
	storageSecret, managedAuthSecret, err := getSecrets(ctx, k, stack, fg)
	if err != nil {
		return storage.Options{}, err
	}

	objStore, err := extractSecrets(stack.Spec.Storage.Secret, storageSecret, managedAuthSecret, fg)
	if err != nil {
		return storage.Options{}, &status.DegradedError{
			Message: fmt.Sprintf("Invalid object storage secret contents: %s", err),
			Reason:  lokiv1.ReasonInvalidObjectStorageSecret,
			Requeue: false,
		}
	}

	if objStore.CredentialMode == lokiv1.CredentialModeManaged && managedAuthSecret == nil {
		// If we have no managed-auth secret at this point, it is an error
		return storage.Options{}, &status.DegradedError{
			Message: "Missing OpenShift cloud credentials secret",
			Reason:  lokiv1.ReasonMissingManagedAuthSecret,
			Requeue: true,
		}
	}

	storageSchemas, err := storage.BuildSchemaConfig(
		time.Now().UTC(),
		stack.Spec.Storage,
		stack.Status.Storage,
	)
	if err != nil {
		return storage.Options{}, &status.DegradedError{
			Message: fmt.Sprintf("Invalid object storage schema contents: %s", err),
			Reason:  lokiv1.ReasonInvalidObjectStorageSchema,
			Requeue: false,
		}
	}

	objStore.Schemas = storageSchemas

	if stack.Spec.Storage.TLS == nil {
		return objStore, nil
	}

	tlsConfig := stack.Spec.Storage.TLS
	if tlsConfig.CA == "" {
		return storage.Options{}, &status.DegradedError{
			Message: "Missing object storage CA config map",
			Reason:  lokiv1.ReasonMissingObjectStorageCAConfigMap,
			Requeue: false,
		}
	}

	cm, err := getCAConfigMap(ctx, k, stack, tlsConfig.CA)
	if err != nil {
		return storage.Options{}, err
	}

	caKey := defaultCAKey
	if tlsConfig.CAKey != "" {
		caKey = tlsConfig.CAKey
	}

	var caHash string
	caHash, err = checkCAConfigMap(cm, caKey)
	if err != nil {
		return storage.Options{}, &status.DegradedError{
			Message: fmt.Sprintf("Invalid object storage CA configmap contents: %s", err),
			Reason:  lokiv1.ReasonInvalidObjectStorageCAConfigMap,
			Requeue: false,
		}
	}

	objStore.SecretSHA1 = fmt.Sprintf("%s;%s", objStore.SecretSHA1, caHash)
	objStore.TLS = &storage.TLSConfig{CA: cm.Name, Key: caKey}

	return objStore, nil
}
