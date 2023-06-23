package validation

import (
	"context"
	"fmt"
	"time"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// objectStorageSchemaMap defines the type for mapping a schema version with a date
type objectStorageSchemaMap map[lokiv1.StorageSchemaEffectiveDate]lokiv1.ObjectStorageSchemaVersion

var _ admission.CustomValidator = &LokiStackValidator{}

// LokiStackValidator implements a custom validator for LokiStack resources.
type LokiStackValidator struct {
	ExtendedValidator func(context.Context, *lokiv1.LokiStack) field.ErrorList
}

// SetupWebhookWithManager registers the LokiStackValidator as a validating webhook
// with the controller-runtime manager or returns an error.
func (v *LokiStackValidator) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&lokiv1.LokiStack{}).
		WithValidator(v).
		Complete()
}

// ValidateCreate implements admission.CustomValidator.
func (v *LokiStackValidator) ValidateCreate(ctx context.Context, obj runtime.Object) error {
	return v.validate(ctx, obj)
}

// ValidateUpdate implements admission.CustomValidator.
func (v *LokiStackValidator) ValidateUpdate(ctx context.Context, _, newObj runtime.Object) error {
	return v.validate(ctx, newObj)
}

// ValidateDelete implements admission.CustomValidator.
func (v *LokiStackValidator) ValidateDelete(_ context.Context, _ runtime.Object) error {
	// No validation on delete
	return nil
}

func (v *LokiStackValidator) validate(ctx context.Context, obj runtime.Object) error {
	stack, ok := obj.(*lokiv1.LokiStack)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("object is not of type LokiStack: %t", obj))
	}

	var allErrs field.ErrorList

	storageStatus := lokiv1.LokiStackStorageStatus{}
	if stack != nil {
		storageStatus = stack.Status.Storage
	}

	errors := ValidateSchemas(&stack.Spec.Storage, time.Now().UTC(), storageStatus)
	if len(errors) != 0 {
		allErrs = append(allErrs, errors...)
	}

	errors = v.validateReplicationSpec(ctx, stack.Spec)
	if len(errors) != 0 {
		allErrs = append(allErrs, errors...)
	}

	if v.ExtendedValidator != nil {
		allErrs = append(allErrs, v.ExtendedValidator(ctx, stack)...)
	}

	if len(allErrs) == 0 {
		return nil
	}

	return apierrors.NewInvalid(
		schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
		stack.Name,
		allErrs,
	)
}

func (v LokiStackValidator) validateReplicationSpec(ctx context.Context, stack lokiv1.LokiStackSpec) field.ErrorList {
	if stack.Replication == nil {
		return nil
	}

	var allErrs field.ErrorList

	// nolint:staticcheck
	if stack.Replication != nil && stack.ReplicationFactor > 0 {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec", "replicationFactor"),
			stack.ReplicationFactor,
			lokiv1.ErrReplicationSpecConflict.Error(),
		))

		return allErrs
	}

	return nil
}

// ValidateSchemas ensures that the schemas are in a valid format
func ValidateSchemas(v *lokiv1.ObjectStorageSpec, utcTime time.Time, status lokiv1.LokiStackStorageStatus) field.ErrorList {
	var allErrs field.ErrorList

	appliedSchemasFound := 0
	containsValidStartDate := false
	found := make(map[lokiv1.StorageSchemaEffectiveDate]bool)

	cutoff := utcTime.Add(lokiv1.StorageSchemaUpdateBuffer)
	appliedSchemas := buildAppliedSchemaMap(status.Schemas, cutoff)

	for i, sc := range v.Schemas {
		if found[sc.EffectiveDate] {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec").Child("storage").Child("schemas").Index(i).Child("effectiveDate"),
				sc.EffectiveDate,
				lokiv1.ErrEffectiveDatesNotUnique.Error(),
			))
		}

		found[sc.EffectiveDate] = true

		date, err := sc.EffectiveDate.UTCTime()
		if err != nil {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec").Child("storage").Child("schemas").Index(i).Child("effectiveDate"),
				sc.EffectiveDate,
				lokiv1.ErrParseEffectiveDates.Error(),
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
				field.NewPath("spec").Child("storage").Child("schemas").Index(i),
				sc,
				lokiv1.ErrSchemaRetroactivelyAdded.Error(),
			))
		}

		if ok && appliedSchemaVersion != sc.Version {
			allErrs = append(allErrs, field.Invalid(
				field.NewPath("spec").Child("storage").Child("schemas").Index(i),
				sc,
				lokiv1.ErrSchemaRetroactivelyChanged.Error(),
			))
		}

		appliedSchemasFound++
	}

	if !containsValidStartDate {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec").Child("storage").Child("schemas"),
			v.Schemas,
			lokiv1.ErrMissingValidStartDate.Error(),
		))
	}

	if appliedSchemasFound != len(appliedSchemas) {
		allErrs = append(allErrs, field.Invalid(
			field.NewPath("spec").Child("storage").Child("schemas"),
			v.Schemas,
			lokiv1.ErrSchemaRetroactivelyRemoved.Error(),
		))
	}

	if len(allErrs) == 0 {
		return nil
	}

	return allErrs
}

// buildAppliedSchemaMap creates a map of schemas which occur before the given time
func buildAppliedSchemaMap(schemas []lokiv1.ObjectStorageSchema, effectiveDate time.Time) objectStorageSchemaMap {
	appliedMap := objectStorageSchemaMap{}

	for _, schema := range schemas {
		date, err := schema.EffectiveDate.UTCTime()

		if err == nil && date.Before(effectiveDate) {
			appliedMap[schema.EffectiveDate] = schema.Version
		}
	}

	return appliedMap
}
