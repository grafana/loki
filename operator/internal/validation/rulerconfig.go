package validation

import (
	"context"
	"fmt"

	lokiv1beta1 "github.com/grafana/loki/operator/apis/loki/v1beta1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var _ admission.CustomValidator = &RulerConfigValidator{}

// RulerConfigValidator implements a custom validator for RulerConfig resources.
type RulerConfigValidator struct {
	ExtendedValidator func(context.Context, *lokiv1beta1.RulerConfig) field.ErrorList
}

// SetupWebhookWithManager registers the RulerConfigValidator as a validating webhook
// with the controller-runtime manager or returns an error.
func (v *RulerConfigValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&lokiv1beta1.RulerConfig{}).
		WithValidator(v).
		Complete()
}

// ValidateCreate implements admission.CustomValidator.
func (v *RulerConfigValidator) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	return v.validate(ctx, obj)
}

// ValidateUpdate implements admission.CustomValidator.
func (v *RulerConfigValidator) ValidateUpdate(ctx context.Context, _, newObj runtime.Object) error {
	return v.validate(ctx, newObj)
}

// ValidateDelete implements admission.CustomValidator.
func (v *RulerConfigValidator) ValidateDelete(_ context.Context, _ runtime.Object) error {
	// No validation on delete
	return nil
}

func (v *RulerConfigValidator) validate(ctx context.Context, obj runtime.Object) error {
	rulerConfig, ok := obj.(*lokiv1beta1.RulerConfig)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("object is not of type RulerConfig: %t", obj))
	}

	var allErrs field.ErrorList

	// Check if header auth is defined in AlertManagerSpec
	am := rulerConfig.Spec.AlertManagerSpec
	if am != nil && am.Client != nil && am.Client.HeaderAuth != nil {
		ha := am.Client.HeaderAuth
		// Credentials and CredentialsFile are mutually exclusive
		if ha.Credentials != nil && ha.CredentialsFile != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("Spec").Child("AlertManagerSpec").Child("Client").Child("HeaderAuth").Child("Credentials"),
				ha.Credentials,
				lokiv1beta1.ErrHeaderAuthCredentialsConflict.Error(),
			))
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("Spec").Child("AlertManagerSpec").Child("Client").Child("HeaderAuth").Child("CredentialsFile"),
				ha.CredentialsFile,
				lokiv1beta1.ErrHeaderAuthCredentialsConflict.Error(),
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
					field.NewPath("Spec").Child("Overrides").Child(tenant).Child("AlertManagerOverrides").Child("Client").Child("HeaderAuth").Child("Credentials"),
					oha.Credentials,
					lokiv1beta1.ErrHeaderAuthCredentialsConflict.Error(),
				))
				allErrs = append(allErrs, field.Invalid(
					field.NewPath("Spec").Child("Overrides").Child(tenant).Child("AlertManagerOverrides").Child("Client").Child("HeaderAuth").Child("CredentialsFile"),
					oha.CredentialsFile,
					lokiv1beta1.ErrHeaderAuthCredentialsConflict.Error(),
				))
			}
		}
	}

	if v.ExtendedValidator != nil {
		allErrs = append(allErrs, v.ExtendedValidator(ctx, rulerConfig)...)
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "loki.grafana.com", Kind: "RulerConfig"},
		rulerConfig.Name,
		allErrs,
	)
}
