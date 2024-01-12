package storage

import (
	"context"
	"fmt"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"
	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	configv1 "github.com/grafana/loki/operator/apis/config/v1"
	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/manifests/storage"
	"github.com/grafana/loki/operator/internal/status"
)

const defaultCAKey = "service-ca.crt"

var hashSeparator = []byte(",")

func BuildOptions(ctx context.Context, k k8s.Client, ll logr.Logger, stack *lokiv1.LokiStack, fg configv1.FeatureGates) (*storage.Options, error) {
	var objStore *storage.Options

	objStoreSecret, managedAuthCreds, err := getSecrets(ctx, k, ll, stack, fg)
	if err != nil {
		return nil, err
	}

	objStore, err = extractSecrets(objStoreSecret, stack.Spec.Storage.Secret.Type, managedAuthCreds)
	if err != nil {
		return nil, &status.DegradedError{
			Message: fmt.Sprintf("Invalid object storage secret contents: %s", err),
			Reason:  lokiv1.ReasonInvalidObjectStorageSecret,
			Requeue: false,
		}
	}
	objStore.OpenShift.Enabled = fg.OpenShift.Enabled

	storageSchemas, err := storage.BuildSchemaConfig(
		time.Now().UTC(),
		stack.Spec.Storage,
		stack.Status.Storage,
	)
	if err != nil {
		return nil, &status.DegradedError{
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
		return nil, &status.DegradedError{
			Message: "Missing object storage CA config map",
			Reason:  lokiv1.ReasonMissingObjectStorageCAConfigMap,
			Requeue: false,
		}
	}

	var cm corev1.ConfigMap
	key := client.ObjectKey{Name: tlsConfig.CA, Namespace: stack.Namespace}
	if err = k.Get(ctx, key, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, &status.DegradedError{
				Message: "Missing object storage CA config map",
				Reason:  lokiv1.ReasonMissingObjectStorageCAConfigMap,
				Requeue: false,
			}
		}
		return nil, kverrors.Wrap(err, "failed to lookup lokistack object storage CA config map", "name", key)
	}

	caKey := defaultCAKey
	if tlsConfig.CAKey != "" {
		caKey = tlsConfig.CAKey
	}

	var caHash string
	caHash, err = checkCAConfigMap(&cm, caKey)
	if err != nil {
		return nil, &status.DegradedError{
			Message: fmt.Sprintf("Invalid object storage CA configmap contents: %s", err),
			Reason:  lokiv1.ReasonInvalidObjectStorageCAConfigMap,
			Requeue: false,
		}
	}

	objStore.SecretSHA1 = fmt.Sprintf("%s;%s", objStore.SecretSHA1, caHash)
	objStore.TLS = &storage.TLSConfig{CA: cm.Name, Key: caKey}

	return objStore, nil
}
