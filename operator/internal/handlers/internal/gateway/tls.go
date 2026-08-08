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

// valueRefValidationContext describes which part of the LokiStack spec a
// ConfigMap/Secret reference validation failure came from, both for the
// human-readable error message (description) and the machine-facing
// Condition Reason (missingReason, invalidReason).
type valueRefValidationContext struct {
	description   string
	missingReason lokiv1.LokiStackConditionReason
	invalidReason lokiv1.LokiStackConditionReason
}

var gatewayTLSValidationContext = valueRefValidationContext{
	description:   "gateway TLS configuration",
	missingReason: lokiv1.ReasonMissingGatewayTLSConfig,
	invalidReason: lokiv1.ReasonInvalidGatewayTLSConfig,
}

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
		if err := validateValueRef(ctx, k, fieldNameCA, stack.Namespace, gatewayTLSValidationContext, tls.CA); err != nil {
			return err
		}
	}

	if tls.Certificate != nil {
		if err := validateValueRef(ctx, k, fieldNameCertificate, stack.Namespace, gatewayTLSValidationContext, tls.Certificate); err != nil {
			return err
		}
	}

	if tls.PrivateKey != nil {
		if err := validateSecretRef(ctx, k, fieldNameKey, stack.Namespace, gatewayTLSValidationContext, tls.PrivateKey.SecretName, tls.PrivateKey.Key); err != nil {
			return err
		}
	}

	return nil
}

// validateValueRef checks that the ConfigMap or Secret referenced by ref exists and
// contains the referenced key. vctx describes which part of the LokiStack spec the
// reference came from, both for the error message text and the Condition Reason.
func validateValueRef(ctx context.Context, k k8s.Client, fieldName, namespace string, vctx valueRefValidationContext, ref *lokiv1.ValueReference) error {
	if ref.ConfigMapName != "" {
		return validateConfigRef(ctx, k, fieldName, namespace, vctx, ref.ConfigMapName, ref.Key)
	}
	if ref.SecretName != "" {
		return validateSecretRef(ctx, k, fieldName, namespace, vctx, ref.SecretName, ref.Key)
	}

	return kverrors.New("invalid call to validateValueRef configmap and secret not set", "field", fieldName, "ref", ref)
}

func validateConfigRef(ctx context.Context, k k8s.Client, fieldName, namespace string, vctx valueRefValidationContext, name, key string) error {
	var cm corev1.ConfigMap

	objKey := client.ObjectKey{Name: name, Namespace: namespace}
	if err := k.Get(ctx, objKey, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			return &status.DegradedError{
				Message: fmt.Sprintf("Missing configmap for field %q in %s: %s", fieldName, vctx.description, name),
				Reason:  vctx.missingReason,
				Requeue: false,
			}
		}
		return kverrors.Wrap(err, fmt.Sprintf("failed to lookup configmap for field %q in %s", fieldName, vctx.description), "key", objKey.String())
	}

	if cm.Data[key] == "" && len(cm.BinaryData[key]) == 0 {
		return &status.DegradedError{
			Message: fmt.Sprintf("Invalid configmap %s for field %q in %s, missing key: %s", name, fieldName, vctx.description, key),
			Reason:  vctx.invalidReason,
			Requeue: false,
		}
	}

	return nil
}

func validateSecretRef(ctx context.Context, k k8s.Client, fieldName, namespace string, vctx valueRefValidationContext, name, key string) error {
	var secret corev1.Secret

	objKey := client.ObjectKey{Name: name, Namespace: namespace}
	if err := k.Get(ctx, objKey, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return &status.DegradedError{
				Message: fmt.Sprintf("Missing secret for field %q in %s: %s", fieldName, vctx.description, name),
				Reason:  vctx.missingReason,
				Requeue: false,
			}
		}
		return kverrors.Wrap(err, fmt.Sprintf("failed to lookup secret for field %q in %s", fieldName, vctx.description), "key", objKey.String())
	}

	if len(secret.Data[key]) == 0 {
		return &status.DegradedError{
			Message: fmt.Sprintf("Invalid secret %s for field %q in %s, missing key: %s", name, fieldName, vctx.description, key),
			Reason:  vctx.invalidReason,
			Requeue: false,
		}
	}

	return nil
}
