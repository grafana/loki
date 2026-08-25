package gateway

import (
	"context"
	"errors"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/external/k8s"
	"github.com/grafana/loki/operator/internal/status"
)

const (
	specPathGatewayTLS           = "spec.tenants.gateway.tls"
	specPathGatewayTLSCA         = "spec.tenants.gateway.tls.ca"
	specPathGatewayTLSCert       = "spec.tenants.gateway.tls.certificate"
	specPathGatewayTLSPrivateKey = "spec.tenants.gateway.tls.privateKey"
	specPathPassthroughCA        = "spec.tenants.passthrough.ca"
)

var (
	errMissing = errors.New("missing resource")
	errInvalid = errors.New("invalid config")
)

func asDegraded(err error, missing, invalid lokiv1.LokiStackConditionReason) error {
	switch {
	case errors.Is(err, errMissing):
		return &status.DegradedError{Message: err.Error(), Reason: missing, Requeue: true}
	case errors.Is(err, errInvalid):
		return &status.DegradedError{Message: err.Error(), Reason: invalid, Requeue: false}
	default:
		return err
	}
}

func validateTLSConfig(ctx context.Context, k k8s.Client, stack *lokiv1.LokiStack) error {
	if stack.Spec.Tenants == nil || stack.Spec.Tenants.Gateway == nil || stack.Spec.Tenants.Gateway.TLS == nil {
		return nil
	}

	tls := stack.Spec.Tenants.Gateway.TLS
	if tls.Certificate == nil || tls.PrivateKey == nil {
		return &status.DegradedError{
			Message: fmt.Sprintf("Invalid configuration, field %s: certificate and privateKey must both be set", specPathGatewayTLS),
			Reason:  lokiv1.ReasonInvalidGatewayTLSConfig,
			Requeue: false,
		}
	}

	missingReason := lokiv1.ReasonMissingGatewayTLSConfig
	invalidReason := lokiv1.ReasonInvalidGatewayTLSConfig

	if tls.CA != nil {
		if err := validateValueRef(ctx, k, specPathGatewayTLSCA, stack.Namespace, tls.CA); err != nil {
			return asDegraded(err, missingReason, invalidReason)
		}
	}

	if err := validateValueRef(ctx, k, specPathGatewayTLSCert, stack.Namespace, tls.Certificate); err != nil {
		return asDegraded(err, missingReason, invalidReason)
	}

	if err := validateSecretRef(ctx, k, specPathGatewayTLSPrivateKey, stack.Namespace, tls.PrivateKey.SecretName, tls.PrivateKey.Key); err != nil {
		return asDegraded(err, missingReason, invalidReason)
	}

	return nil
}

func validatePassthroughCA(ctx context.Context, k k8s.Client, httpEncryption bool, stack *lokiv1.LokiStack) error {
	if !httpEncryption {
		// TODO(JoaoBraveCoding): Discuss with @xperimental if this makes sense or if we should always require
		// mTLS with the client
		return nil // If HTTP encryption is not enabled, we do not require clients to provide a certificate
	}

	if stack.Spec.Tenants.Passthrough == nil || stack.Spec.Tenants.Passthrough.CA == nil {
		return &status.DegradedError{
			Message: fmt.Sprintf("Invalid configuration, field %s must be configured", specPathPassthroughCA),
			Reason:  lokiv1.ReasonInvalidPassthroughConfiguration,
			Requeue: false,
		}
	}

	err := validateValueRef(ctx, k, specPathPassthroughCA, stack.Namespace, stack.Spec.Tenants.Passthrough.CA)
	if err != nil {
		return asDegraded(err, lokiv1.ReasonMissingPassthroughConfiguration, lokiv1.ReasonInvalidPassthroughConfiguration)
	}

	return nil
}

func validateValueRef(ctx context.Context, k k8s.Client, specPath, namespace string, ref *lokiv1.ValueReference) error {
	if ref.ConfigMapName != "" {
		return validateConfigRef(ctx, k, specPath, namespace, ref.ConfigMapName, ref.Key)
	}
	if ref.SecretName != "" {
		return validateSecretRef(ctx, k, specPath, namespace, ref.SecretName, ref.Key)
	}

	//nolint:staticcheck // capitalized for LokiStack status message
	return fmt.Errorf("Invalid config, configMapName and secretName are not set in field %s: %w", specPath, errInvalid)
}

func validateConfigRef(ctx context.Context, k k8s.Client, specPath, namespace, name, key string) error {
	var cm corev1.ConfigMap

	objKey := client.ObjectKey{Name: name, Namespace: namespace}
	if err := k.Get(ctx, objKey, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			//nolint:staticcheck // capitalized for LokiStack status message
			return fmt.Errorf("Missing ConfigMap %q referenced by field %s: %w", name, specPath, errMissing)
		}
		return err
	}

	if cm.Data[key] == "" && len(cm.BinaryData[key]) == 0 {
		//nolint:staticcheck // capitalized for LokiStack status message
		return fmt.Errorf("Invalid key %q in ConfigMap %q referenced by field %s, missing or empty: %w", key, name, specPath, errInvalid)
	}

	return nil
}

func validateSecretRef(ctx context.Context, k k8s.Client, specPath, namespace, name, key string) error {
	var secret corev1.Secret

	objKey := client.ObjectKey{Name: name, Namespace: namespace}
	if err := k.Get(ctx, objKey, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			//nolint:staticcheck // capitalized for LokiStack status message
			return fmt.Errorf("Missing Secret %q referenced by field %s: %w", name, specPath, errMissing)
		}
		return err
	}

	if len(secret.Data[key]) == 0 {
		//nolint:staticcheck // capitalized for LokiStack status message
		return fmt.Errorf("Invalid key %q in Secret %q referenced by field %s, missing or empty: %w", key, name, specPath, errInvalid)
	}

	return nil
}
