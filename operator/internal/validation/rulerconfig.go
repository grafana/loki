package validation

import (
	"context"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

var _ admission.Validator[*lokiv1.RulerConfig] = &RulerConfigValidator{}

// RulerConfigValidator implements a custom validator for RulerConfig resources.
type RulerConfigValidator struct{}

// SetupWebhookWithManager registers the RulerConfigValidator as a validating webhook
// with the controller-runtime manager or returns an error.
func (v *RulerConfigValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &lokiv1.RulerConfig{}).
		WithValidator(v).
		Complete()
}

// ValidateCreate implements admission.Validator.
func (v *RulerConfigValidator) ValidateCreate(ctx context.Context, obj *lokiv1.RulerConfig) (admission.Warnings, error) {
	return v.validate(ctx, obj)
}

// ValidateUpdate implements admission.Validator.
func (v *RulerConfigValidator) ValidateUpdate(ctx context.Context, _, newObj *lokiv1.RulerConfig) (admission.Warnings, error) {
	return v.validate(ctx, newObj)
}

// ValidateDelete implements admission.Validator.
func (v *RulerConfigValidator) ValidateDelete(_ context.Context, _ *lokiv1.RulerConfig) (admission.Warnings, error) {
	// No validation on delete
	return nil, nil
}

func (v *RulerConfigValidator) validate(_ context.Context, rulerConfig *lokiv1.RulerConfig) (admission.Warnings, error) {
	var allErrs field.ErrorList

	// Check if header auth is defined in AlertManagerSpec
	am := rulerConfig.Spec.AlertManagerSpec
	if am != nil && am.Client != nil && am.Client.HeaderAuth != nil {
		ha := am.Client.HeaderAuth
		// Credentials and CredentialsFile are mutually exclusive
		if ha.Credentials != nil && ha.CredentialsFile != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "alertmanager", "client", "headerAuth", "credentials"),
				ha.Credentials,
				lokiv1.ErrHeaderAuthCredentialsConflict.Error(),
			))
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec", "alertmanager", "client", "headerAuth", "credentialsFile"),
				ha.CredentialsFile,
				lokiv1.ErrHeaderAuthCredentialsConflict.Error(),
			))
		}
	}

	// Check if header auth is defined in AlertManagerOverrides
	for tenant, override := range rulerConfig.Spec.Overrides {
		amo := override.AlertManagerOverrides
		if amo != nil && amo.Client != nil && amo.Client.HeaderAuth != nil {
			oha := amo.Client.HeaderAuth
			// Credentials and CredentialsFile are mutually exclusive
			if oha.Credentials != nil && oha.CredentialsFile != nil {
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec", "overrides", tenant, "alertmanager", "client", "headerAuth", "credentials"),
					oha.Credentials,
					lokiv1.ErrHeaderAuthCredentialsConflict.Error(),
				))
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("spec", "overrides", tenant, "alertmanager", "client", "headerAuth", "credentialsFile"),
					oha.CredentialsFile,
					lokiv1.ErrHeaderAuthCredentialsConflict.Error(),
				))
			}
		}
	}

	if len(allErrs) == 0 {
		return nil, nil
	}

	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: "loki.grafana.com", Kind: "RulerConfig"},
		rulerConfig.Name,
		allErrs,
	)
}
