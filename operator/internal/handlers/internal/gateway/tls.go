package gateway

import (
	"context"
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
	fieldNameCA          = "ca"
	fieldNameCertificate = "certificate"
	fieldNameKey         = "privateKey"
)

func validateTLSConfig(ctx context.Context, k k8s.Client, stack *lokiv1.LokiStack) error {
	if stack.Spec.Tenants == nil || stack.Spec.Tenants.Gateway == nil || stack.Spec.Tenants.Gateway.TLS == nil {
		return nil
	}

	tls := stack.Spec.Tenants.Gateway.TLS
	if tls.Certificate == nil || tls.PrivateKey == nil {
		return &status.DegradedError{
			Message: "Missing certificate or key in gateway TLS configuration. Please provide both certificate and key.",
			Reason:  lokiv1.ReasonInvalidGatewayTLSConfig,
			Requeue: false,
		}
	}

	if tls.CA != nil {
		if err := validateValueRef(ctx, k, fieldNameCA, stack.Namespace, tls.CA); err != nil {
			return err
		}
	}

	if tls.Certificate != nil {
		if err := validateValueRef(ctx, k, fieldNameCertificate, stack.Namespace, tls.Certificate); err != nil {
			return err
		}
	}

	if tls.PrivateKey != nil {
		if err := validateSecretRef(ctx, k, fieldNameKey, stack.Namespace, tls.PrivateKey.SecretName, tls.PrivateKey.Key); err != nil {
			return err
		}
	}

	return nil
}

func validateValueRef(ctx context.Context, k k8s.Client, fieldName, namespace string, ref *lokiv1.ValueReference) error {
	if ref.ConfigMapName != "" {
		return validateConfigRef(ctx, k, fieldName, namespace, ref.ConfigMapName, ref.Key)
	}
	if ref.SecretName != "" {
		return validateSecretRef(ctx, k, fieldName, namespace, ref.SecretName, ref.Key)
	}

	return kverrors.New("invalid call to validateValueRef configmap and secret not set", "field", fieldName, "ref", ref)
}

func validateConfigRef(ctx context.Context, k k8s.Client, fieldName, namespace, name, key string) error {
	var cm corev1.ConfigMap

	objKey := client.ObjectKey{Name: name, Namespace: namespace}
	if err := k.Get(ctx, objKey, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			return &status.DegradedError{
				Message: fmt.Sprintf("Missing configmap for field %q in gateway TLS configuration: %s", fieldName, name),
				Reason:  lokiv1.ReasonMissingGatewayTLSConfig,
				Requeue: false,
			}
		}
		return kverrors.Wrap(err, fmt.Sprintf("failed to lookup configmap for field %q in gateway TLS configuration", fieldName), "key", objKey.String())
	}

	if cm.Data[key] == "" && len(cm.BinaryData[key]) == 0 {
		return &status.DegradedError{
			Message: fmt.Sprintf("Invalid configmap %s for field %q in gateway TLS configuration, missing key: %s", name, fieldName, key),
			Reason:  lokiv1.ReasonInvalidGatewayTLSConfig,
			Requeue: false,
		}
	}

	return nil
}

func validateSecretRef(ctx context.Context, k k8s.Client, fieldName, namespace, name, key string) error {
	var secret corev1.Secret

	objKey := client.ObjectKey{Name: name, Namespace: namespace}
	if err := k.Get(ctx, objKey, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return &status.DegradedError{
				Message: fmt.Sprintf("Missing secret for field %q in gateway TLS configuration: %s", fieldName, name),
				Reason:  lokiv1.ReasonMissingGatewayTLSConfig,
				Requeue: false,
			}
		}
		return kverrors.Wrap(err, fmt.Sprintf("failed to lookup secret for field %q in gateway TLS configuration", fieldName), "key", objKey.String())
	}

	if len(secret.Data[key]) == 0 {
		return &status.DegradedError{
			Message: fmt.Sprintf("Invalid secret %s for field %q in gateway TLS configuration, missing key: %s", name, fieldName, key),
			Reason:  lokiv1.ReasonInvalidGatewayTLSConfig,
			Requeue: false,
		}
	}

	return nil
}
