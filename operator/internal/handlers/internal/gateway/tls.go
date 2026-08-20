package gateway

import (
	"context"
	"errors"
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

// valueRefFailure describes which part of the LokiStack spec a ConfigMap/Secret
// reference validation failure came from, both for the human-readable error
// message (description) and the machine-facing Condition Reason (missingReason,
// invalidReason).
type valueRefFailure struct {
	description   string
	missingReason lokiv1.LokiStackConditionReason
	invalidReason lokiv1.LokiStackConditionReason
}

var gatewayTLSValidationContext = valueRefFailure{
	description:   "gateway TLS configuration",
	missingReason: lokiv1.ReasonMissingGatewayTLSConfig,
	invalidReason: lokiv1.ReasonInvalidGatewayTLSConfig,
}

// passthroughCAValidationContext reuses ReasonInvalidPassthroughConfiguration for both
// the missing and invalid cases: unlike gateway TLS, passthrough currently has only one
// Reason for "something is wrong with the passthrough config", and reusing it keeps all
// passthrough CA failures (nil field, missing ConfigMap/Secret, missing key) consistent
// with each other and distinct from the gateway-TLS-specific Reasons.
var passthroughCAValidationContext = valueRefFailure{
	description:   "passthrough gateway configuration",
	missingReason: lokiv1.ReasonMissingPassthroughConfiguration,
	invalidReason: lokiv1.ReasonInvalidPassthroughConfiguration,
}

func validateTLSConfig(ctx context.Context, k k8s.Client, stack *lokiv1.LokiStack) error {
	if stack.Spec.Tenants == nil || stack.Spec.Tenants.Gateway == nil || stack.Spec.Tenants.Gateway.TLS == nil {
		return nil
	}

	tls := stack.Spec.Tenants.Gateway.TLS
	if tls.Certificate == nil || tls.PrivateKey == nil {
		return &status.DegradedError{
			Message: fmt.Sprintf("Missing certificate or key in %s. Please provide both certificate and key.", gatewayTLSValidationContext.description),
			Reason:  gatewayTLSValidationContext.invalidReason,
			// Requeue: false, unlike the ConfigMap/Secret lookups below, because
			// fixing this requires editing the LokiStack spec itself, which is
			// already watched and will trigger its own reconcile.
			Requeue: false,
		}
	}

	if tls.CA != nil {
		err := validateValueRef(ctx, k, fieldNameCA, stack.Namespace, gatewayTLSValidationContext.description, tls.CA)
		if err := toDegradedError(err, fieldNameCA, gatewayTLSValidationContext); err != nil {
			return err
		}
	}

	if tls.Certificate != nil {
		err := validateValueRef(ctx, k, fieldNameCertificate, stack.Namespace, gatewayTLSValidationContext.description, tls.Certificate)
		if err := toDegradedError(err, fieldNameCertificate, gatewayTLSValidationContext); err != nil {
			return err
		}
	}

	if tls.PrivateKey != nil {
		err := validateSecretRef(ctx, k, fieldNameKey, stack.Namespace, gatewayTLSValidationContext.description, tls.PrivateKey.SecretName, tls.PrivateKey.Key)
		if err := toDegradedError(err, fieldNameKey, gatewayTLSValidationContext); err != nil {
			return err
		}
	}

	return nil
}

// refLookupKind identifies why a ConfigMap/Secret reference failed validation.
type refLookupKind int

const (
	refMissing refLookupKind = iota
	refInvalid
)

// refLookupError reports that a ConfigMap or Secret referenced from the LokiStack
// spec failed validation. It only carries the raw lookup facts (which kind of
// resource, its name, and the key that was checked): validateValueRef,
// validateConfigRef and validateSecretRef are shared by both gateway TLS and
// passthrough CA validation, which use different messages and Condition Reasons,
// so turning this into a status.DegradedError is left to the caller via
// toDegradedError.
type refLookupError struct {
	kind         refLookupKind
	resourceKind string // "configmap" or "secret"
	name         string
	key          string
}

func (e *refLookupError) Error() string {
	if e.kind == refMissing {
		return fmt.Sprintf("missing %s: %s", e.resourceKind, e.name)
	}
	return fmt.Sprintf("%s %s missing key: %s", e.resourceKind, e.name, e.key)
}

// requeueOnMissingRef is the Requeue policy for gateway TLS and passthrough CA
// reference errors. This is false, matching storage CA: the LokiStack controller
// watches ConfigMaps/Secrets referenced by gateway TLS and passthrough CA (see
// enqueueForCAConfigMap/enqueueForCASecret), so fixing their contents (e.g.
// adding a missing key) retriggers reconciliation without requiring
// requeue-with-backoff.
const requeueOnMissingRef = false

// toDegradedError converts a refLookupError coming from validateValueRef,
// validateConfigRef or validateSecretRef into a status.DegradedError, using
// fieldName and vRefFailure to describe which part of the LokiStack spec the
// reference came from. Any other error (e.g. a non-NotFound API failure) is
// returned unchanged, since it isn't something the user can fix by editing the
// referenced ConfigMap/Secret.
func toDegradedError(err error, fieldName string, vRefFailure valueRefFailure) error {
	if err == nil {
		return nil
	}

	var refErr *refLookupError
	if !errors.As(err, &refErr) {
		return err
	}

	if refErr.kind == refMissing {
		return &status.DegradedError{
			Message: fmt.Sprintf("Missing %s for field %q in %s: %s", refErr.resourceKind, fieldName, vRefFailure.description, refErr.name),
			Reason:  vRefFailure.missingReason,
			Requeue: requeueOnMissingRef,
		}
	}

	return &status.DegradedError{
		Message: fmt.Sprintf("Invalid %s %s for field %q in %s, missing key: %s", refErr.resourceKind, refErr.name, fieldName, vRefFailure.description, refErr.key),
		Reason:  vRefFailure.invalidReason,
		Requeue: requeueOnMissingRef,
	}
}

// validateValueRef checks that the ConfigMap or Secret referenced by ref exists and
// contains the referenced key. description is used only to annotate the error
// message if the lookup itself fails unexpectedly (e.g. a non-NotFound API error).
func validateValueRef(ctx context.Context, k k8s.Client, fieldName, namespace, description string, ref *lokiv1.ValueReference) error {
	if ref.ConfigMapName != "" {
		return validateConfigRef(ctx, k, fieldName, namespace, description, ref.ConfigMapName, ref.Key)
	}
	if ref.SecretName != "" {
		return validateSecretRef(ctx, k, fieldName, namespace, description, ref.SecretName, ref.Key)
	}

	return kverrors.New("invalid call to validateValueRef configmap and secret not set", "field", fieldName, "ref", ref)
}

func validateConfigRef(ctx context.Context, k k8s.Client, fieldName, namespace, description, name, key string) error {
	var cm corev1.ConfigMap

	objKey := client.ObjectKey{Name: name, Namespace: namespace}
	if err := k.Get(ctx, objKey, &cm); err != nil {
		if apierrors.IsNotFound(err) {
			return &refLookupError{kind: refMissing, resourceKind: "configmap", name: name}
		}
		return kverrors.Wrap(err, fmt.Sprintf("failed to lookup configmap for field %q in %s", fieldName, description), "key", objKey.String())
	}

	if cm.Data[key] == "" && len(cm.BinaryData[key]) == 0 {
		return &refLookupError{kind: refInvalid, resourceKind: "configmap", name: name, key: key}
	}

	return nil
}

func validateSecretRef(ctx context.Context, k k8s.Client, fieldName, namespace, description, name, key string) error {
	var secret corev1.Secret

	objKey := client.ObjectKey{Name: name, Namespace: namespace}
	if err := k.Get(ctx, objKey, &secret); err != nil {
		if apierrors.IsNotFound(err) {
			return &refLookupError{kind: refMissing, resourceKind: "secret", name: name}
		}
		return kverrors.Wrap(err, fmt.Sprintf("failed to lookup secret for field %q in %s", fieldName, description), "key", objKey.String())
	}

	if len(secret.Data[key]) == 0 {
		return &refLookupError{kind: refInvalid, resourceKind: "secret", name: name, key: key}
	}

	return nil
}
