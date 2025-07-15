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

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
	"github.com/grafana/loki/operator/internal/validation/openshift"
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
func (v *LokiStackValidator) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, obj)
}

// ValidateUpdate implements admission.CustomValidator.
func (v *LokiStackValidator) ValidateUpdate(ctx context.Context, _, newObj runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx, newObj)
}

// ValidateDelete implements admission.CustomValidator.
func (v *LokiStackValidator) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	// No validation on delete
	return nil, nil
}

func (v *LokiStackValidator) validate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	stack, ok := obj.(*lokiv1.LokiStack)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("object is not of type LokiStack: %t", obj))
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

	if stack.Spec.Limits != nil {
		if (stack.Spec.Limits.Global != nil && stack.Spec.Limits.Global.OTLP != nil) ||
			len(stack.Spec.Limits.Tenants) > 0 {
			// Only need to validate custom OTLP configuration
			allErrs = append(allErrs, v.validateOTLPConfiguration(&stack.Spec)...)
		}
	}

	if v.ExtendedValidator != nil {
		allErrs = append(allErrs, v.ExtendedValidator(ctx, stack)...)
	}

	if len(allErrs) == 0 {
		return nil, nil
	}

	return nil, apierrors.NewInvalid(
		schema.GroupKind{Group: "loki.grafana.com", Kind: "LokiStack"},
		stack.Name,
		allErrs,
	)
}

func (v *LokiStackValidator) validateOTLPConfiguration(spec *lokiv1.LokiStackSpec) field.ErrorList {
	if spec.Tenants == nil {
		return nil
	}

	if spec.Tenants.Mode == lokiv1.OpenshiftLogging {
		// This tenancy mode always provides stream labels
		return openshift.ValidateOTLPInvalidDrop(spec)
	}

	if spec.Tenants.Mode == lokiv1.OpenshiftNetwork {
		// No validation defined for openshift-network tenancy mode
		// TODO can we define a validation for this mode?
		return nil
	}

	if spec.Limits == nil {
		return nil
	}

	hasGlobalStreamLabels := false
	errList := field.ErrorList{}
	var globalOtlp *lokiv1.OTLPSpec
	if spec.Limits.Global != nil && spec.Limits.Global.OTLP != nil {
		globalOtlp = spec.Limits.Global.OTLP

		hasGlobalStreamLabels = v.hasOTLPStreamLabel(globalOtlp)
		errList = append(errList, v.checkOTLPInvalidDrop(field.NewPath("spec", "limits", "global", "otlp"), globalOtlp, nil)...)
	}

	if !hasGlobalStreamLabels && spec.Limits.Tenants == nil {
		// No tenant config and no global stream labels -> error
		errList = append(errList, field.Invalid(
			field.NewPath("spec", "limits", "global", "otlp", "streamLabels", "resourceAttributes"),
			nil,
			lokiv1.ErrOTLPGlobalNoStreamLabel.Error(),
		))
	}

	errList = append(errList, v.validateOTLPTenantConfiguration(spec, globalOtlp, hasGlobalStreamLabels)...)
	return errList
}

func (v *LokiStackValidator) validateOTLPTenantConfiguration(spec *lokiv1.LokiStackSpec, globalOtlp *lokiv1.OTLPSpec, hasGlobalStreamLabel bool) (errList field.ErrorList) {
	if spec.Limits == nil || spec.Limits.Tenants == nil {
		return nil
	}

	errList = field.ErrorList{}
	for _, tenant := range spec.Tenants.Authentication {
		tenantName := tenant.TenantName
		tenantLimits, ok := spec.Limits.Tenants[tenantName]
		if !ok || tenantLimits.OTLP == nil {
			if !hasGlobalStreamLabel {
				// No tenant limits defined and no global stream labels -> error
				errList = append(errList, field.Invalid(
					field.NewPath("spec", "limits", "tenants", tenantName, "otlp"),
					nil,
					lokiv1.ErrOTLPTenantMissing.Error(),
				))
			}

			continue
		}

		if !hasGlobalStreamLabel && !v.hasOTLPStreamLabel(tenantLimits.OTLP) {
			errList = append(errList, field.Invalid(
				field.NewPath("spec", "limits", "tenants", tenantName, "otlp", "streamLabels", "resourceAttributes"),
				nil,
				lokiv1.ErrOTLPTenantNoStreamLabel.Error(),
			))
		}

		errList = append(errList, v.checkOTLPInvalidDrop(field.NewPath("spec", "limits", "tenants", tenantName, "otlp"), tenantLimits.OTLP, globalOtlp)...)
	}

	return errList
}

func (v *LokiStackValidator) hasOTLPStreamLabel(otlp *lokiv1.OTLPSpec) bool {
	if otlp == nil {
		return false
	}

	if otlp.StreamLabels == nil {
		return false
	}

	return len(otlp.StreamLabels.ResourceAttributes) > 0
}

func (v *LokiStackValidator) checkOTLPInvalidDrop(basePath *field.Path, otlp, inheritedOtlp *lokiv1.OTLPSpec) field.ErrorList {
	if otlp.Drop == nil {
		return nil
	}

	errList := field.ErrorList{}
	streamAttributes := [][]lokiv1.OTLPAttributeReference{}
	if streamLabels := otlp.StreamLabels; streamLabels != nil {
		streamAttributes = append(streamAttributes, streamLabels.ResourceAttributes)
	}
	if inheritedOtlp != nil && inheritedOtlp.StreamLabels != nil && len(inheritedOtlp.StreamLabels.ResourceAttributes) > 0 {
		streamAttributes = append(streamAttributes, inheritedOtlp.StreamLabels.ResourceAttributes)
	}
	errList = append(errList, v.checkOTLPInvalidDropReference(
		basePath.Child("drop", "resourceAttributes"),
		otlp.Drop.ResourceAttributes,
		streamAttributes,
	)...)

	return errList
}

func (v *LokiStackValidator) checkOTLPInvalidDropReference(basePath *field.Path, dropList []lokiv1.OTLPAttributeReference, keepLists [][]lokiv1.OTLPAttributeReference) field.ErrorList {
	if len(dropList) == 0 {
		return nil
	}

	if len(keepLists) == 0 {
		return nil
	}

	attributeNames := map[string]bool{}
	errList := field.ErrorList{}

	for _, keeps := range keepLists {
		for _, attr := range keeps {
			if attr.Regex {
				// skip regular expressions for this check
				continue
			}
			attributeNames[attr.Name] = true
		}
	}

	for i, attr := range dropList {
		if attr.Regex {
			continue
		}

		if !attributeNames[attr.Name] {
			continue
		}

		errList = append(errList, field.Invalid(
			basePath.Index(i),
			attr.Name,
			lokiv1.ErrOTLPInvalidDrop.Error(),
		))
	}

	return errList
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
