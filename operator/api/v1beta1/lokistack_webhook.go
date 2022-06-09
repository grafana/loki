package v1beta1

import (
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

const (
	// DateTimeFormat is the datetime string need to format the time.
	DateTimeFormat = "2006-01-02"
)

// SetupWebhookWithManager registers the Lokistack to the controller-runtime manager
// or returns an error.
func (s *LokiStack) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(s).
		Complete()
}

//+kubebuilder:webhook:path=/validate-loki-grafana-com-v1beta1-lokistack,mutating=false,failurePolicy=fail,sideEffects=None,groups=loki.grafana.com,resources=lokistack,verbs=create;update,versions=v1beta1,name=vlokistack.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &LokiStack{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (s *LokiStack) ValidateCreate() error {
	return s.validate()
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (s *LokiStack) ValidateUpdate(_ runtime.Object) error {
	return s.validate()
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (s *LokiStack) ValidateDelete() error {
	// Do nothing
	return nil
}

func (s *LokiStack) validate() error {
	var allErrs field.ErrorList

	found := make(map[StorageSchemaEffectiveDate]bool)

	for i, sc := range s.Spec.Storage.Schemas {
		if found[sc.EffectiveDate] {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("Spec").Child("Storage").Child("Schemas").Index(i).Child("EffectiveDate"),
				sc.EffectiveDate,
				ErrEffectiveDatesNotUnique.Error(),
			))
		}

		found[sc.EffectiveDate] = true

		if _, err := time.Parse(DateTimeFormat, string(sc.EffectiveDate)); err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("Spec").Child("Storage").Child("Schemas").Index(i).Child("EffectiveDate"),
				sc.EffectiveDate,
				ErrParseEffectiveDates.Error(),
			))
		}
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
		s.Name,
		allErrs,
	)
}
