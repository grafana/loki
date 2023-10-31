package validation

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	lokiv1 "github.com/grafana/loki/operator/apis/loki/v1"
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

	errors = v.validateReplicationSpec(stack.Spec)
	if len(errors) != 0 {
		allErrs = append(allErrs, errors...)
	}

	errors = v.validateHashRingSpec(stack.Spec)
	if len(errors) != 0 {
		allErrs = append(allErrs, errors...)
	}

	errors = v.validateLimitsSpec(stack.Spec.Limits)
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

func (v *LokiStackValidator) validateHashRingSpec(s lokiv1.LokiStackSpec) field.ErrorList {
	if s.HashRing == nil {
		return nil
	}

	if s.HashRing.MemberList == nil {
		return nil
	}

	if s.HashRing.MemberList.EnableIPv6 && s.HashRing.MemberList.InstanceAddrType == lokiv1.InstanceAddrDefault {
		return field.ErrorList{
			field.Invalid(
				field.NewPath("spec", "hashRing", "memberlist", "instanceAddrType"),
				s.HashRing.MemberList.InstanceAddrType,
				lokiv1.ErrIPv6InstanceAddrTypeNotAllowed.Error(),
			),
		}
	}

	return nil
}

func (v *LokiStackValidator) validateLimitsSpec(s *lokiv1.LimitsSpec) field.ErrorList {
	if s == nil {
		return nil
	}

	if s.Global == nil && s.Tenants == nil {
		return nil
	}

	var errors field.ErrorList
	if s.Global != nil && s.Global.IngestionLimits != nil {
		fp := field.NewPath("spec", "limits", "global", "ingestion", "desiredRate")
		errors = append(errors, validateIngestionLimits(s.Global.IngestionLimits, fp)...)
	}

	for id, tenant := range s.Tenants {
		if tenant.IngestionLimits == nil {
			continue
		}

		fp := field.NewPath("spec", "limits", "tenants", id, "ingestion", "desiredRate")

		if s.Global == nil {
			errors = append(errors, validateIngestionLimits(tenant.IngestionLimits, fp)...)
		} else {
			errors = append(errors, validateTenantIngestionLimits(s.Global.IngestionLimits, tenant.IngestionLimits, id, fp)...)
		}
	}

	return errors
}

// validateIngestionLimits checks if a given IngestionLimitSpec is valid for:
// - Fields `desiredRate` and `perStreamRateLimit` not both set at a time.
func validateIngestionLimits(spec *lokiv1.IngestionLimitSpec, invalidFieldPath *field.Path) (errs field.ErrorList) {
	// Check against per stream rate limit
	if spec.DesiredRate > 0 && spec.PerStreamRateLimit > 0 {
		errs = append(errs, field.Invalid(
			invalidFieldPath,
			spec.DesiredRate,
			lokiv1.ErrDesiredRateAndPerStreamRateNotAllowed.Error(),
		))
	}

	return errs
}

// validateTenantIngestionLimits checks if a given tenant IngestionLimitSpec is valid for:
// - Tenant fields `desiredRate` and `perStreamRateLimit` not both set at a time.
// - Global field `desiredRate` and tenant field `perStreamRateLimit` not both set at a time.
// - Tenant field `desiredRate` and global field `perStreamRateLimit` not both set at a time.
func validateTenantIngestionLimits(global, tenants *lokiv1.IngestionLimitSpec, id string, fp *field.Path) (errs field.ErrorList) {
	if tenants == nil {
		return nil
	}

	errs = append(errs, validateIngestionLimits(tenants, fp)...)

	if global == nil {
		return errs
	}

	// Check if both global desired rate and tenant per stream rate limit set
	if global.DesiredRate > 0 && tenants.PerStreamRateLimit > 0 {
		errs = append(errs, field.Invalid(
			field.NewPath("spec", "limits", "tenants", id, "ingestion", "perStreamRateLimit"),
			tenants.PerStreamRateLimit,
			lokiv1.ErrDesiredRateAndPerStreamRateNotAllowed.Error(),
		))
	}

	// Check if both tenant desired rate and global per stream rate limit set
	if tenants.DesiredRate > 0 && global.PerStreamRateLimit > 0 {
		errs = append(errs, field.Invalid(
			field.NewPath("spec", "limits", "global", "ingestion", "perStreamRateLimit"),
			global.PerStreamRateLimit,
			lokiv1.ErrDesiredRateAndPerStreamRateNotAllowed.Error(),
		))
	}

	return errs
}

func (v *LokiStackValidator) validateReplicationSpec(stack lokiv1.LokiStackSpec) field.ErrorList {
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
