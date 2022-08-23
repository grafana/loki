package v1

import (
	"reflect"
	"time"

	"github.com/ViaQ/logerr/v2/kverrors"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
)

// objectStorageSchemaMap defines the type for mapping a schema version with a date
type objectStorageSchemaMap map[StorageSchemaEffectiveDate]ObjectStorageSchemaVersion

// SetupWebhookWithManager registers the Lokistack to the controller-runtime manager
// or returns an error.
func (r *LokiStack) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/validate-loki-grafana-com-v1-lokistack,mutating=false,failurePolicy=fail,sideEffects=None,groups=loki.grafana.com,resources=lokistacks,verbs=create;update,versions=v1,name=vlokistack.loki.grafana.com,admissionReviewVersions=v1

var _ webhook.Validator = &LokiStack{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *LokiStack) ValidateCreate() error {
	return r.validate(nil)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *LokiStack) ValidateUpdate(old runtime.Object) error {
	oldStack, ok := old.(*LokiStack)
	if !ok {
		t := reflect.TypeOf(old).String()
		return apierrors.NewInternalError(kverrors.New("runtime object is incorrect type", "type", t))
	}
	return r.validate(oldStack)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *LokiStack) ValidateDelete() error {
	// Do nothing
	return nil
}

// ValidateSchemas ensures that the schemas are in a valid format
func (s *ObjectStorageSpec) ValidateSchemas(utcTime time.Time, status LokiStackStorageStatus) field.ErrorList {
	var allErrs field.ErrorList

	appliedSchemasFound := 0
	containsValidStartDate := false
	found := make(map[StorageSchemaEffectiveDate]bool)

	cutoff := utcTime.Add(StorageSchemaUpdateBuffer)
	appliedSchemas := buildAppliedSchemaMap(status.Schemas, cutoff)

	for i, sc := range s.Schemas {
		if found[sc.EffectiveDate] {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("Spec").Child("Storage").Child("Schemas").Index(i).Child("EffectiveDate"),
				sc.EffectiveDate,
				ErrEffectiveDatesNotUnique.Error(),
			))
		}

		found[sc.EffectiveDate] = true

		date, err := sc.EffectiveDate.UTCTime()
		if err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("Spec").Child("Storage").Child("Schemas").Index(i).Child("EffectiveDate"),
				sc.EffectiveDate,
				ErrParseEffectiveDates.Error(),
			))
		}

		if date.Before(cutoff) {
			containsValidStartDate = true
		}

		// No statuses to compare against or this is a new schema which will be added.
		if len(appliedSchemas) == 0 || date.After(cutoff) {
			continue
		}

		appliedSchemaVersion, ok := appliedSchemas[sc.EffectiveDate]

		if !ok {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("Spec").Child("Storage").Child("Schemas").Index(i),
				sc,
				ErrSchemaRetroactivelyAdded.Error(),
			))
		}

		if ok && appliedSchemaVersion != sc.Version {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("Spec").Child("Storage").Child("Schemas").Index(i),
				sc,
				ErrSchemaRetroactivelyChanged.Error(),
			))
		}

		appliedSchemasFound++
	}

	if !containsValidStartDate {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("Spec").Child("Storage").Child("Schemas"),
			s.Schemas,
			ErrMissingValidStartDate.Error(),
		))
	}

	if appliedSchemasFound != len(appliedSchemas) {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("Spec").Child("Storage").Child("Schemas"),
			s.Schemas,
			ErrSchemaRetroactivelyRemoved.Error(),
		))
	}

	if len(allErrs) == 0 {
		return nil
	}

	return allErrs
}

func (r *LokiStack) validate(old *LokiStack) error {
	var allErrs field.ErrorList

	storageStatus := LokiStackStorageStatus{}
	if old != nil {
		storageStatus = old.Status.Storage
	}

	errors := r.Spec.Storage.ValidateSchemas(time.Now().UTC(), storageStatus)
	if len(errors) != 0 {
		allErrs = append(allErrs, errors...)
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
		r.Name,
		allErrs,
	)
}

// buildAppliedSchemaMap creates a map of schemas which occur before the given time
func buildAppliedSchemaMap(schemas []ObjectStorageSchema, effectiveDate time.Time) objectStorageSchemaMap {
	appliedMap := objectStorageSchemaMap{}

	for _, schema := range schemas {
		date, err := schema.EffectiveDate.UTCTime()

		if err == nil && date.Before(effectiveDate) {
			appliedMap[schema.EffectiveDate] = schema.Version
		}
	}

	return appliedMap
}
