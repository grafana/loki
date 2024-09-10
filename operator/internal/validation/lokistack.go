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
		if stack.Spec.Limits.Global != nil && stack.Spec.Limits.Global.OTLP != nil {
			allErrs = append(allErrs, v.validateGlobalOTLPSpec(stack.Spec.Limits.Global.OTLP)...)
		}

		if stack.Spec.Limits.Tenants != nil {
			allErrs = append(allErrs, v.validatePerTenantOTLPSpec(stack.Spec.Limits.Tenants)...)
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

func (v *LokiStackValidator) validateGlobalOTLPSpec(s *lokiv1.GlobalOTLPSpec) field.ErrorList {
	basePath := field.NewPath("spec", "limits", "global")

	return v.validateOTLPSpec(basePath, &s.OTLPSpec)
}

func (v *LokiStackValidator) validatePerTenantOTLPSpec(tenants map[string]lokiv1.PerTenantLimitsTemplateSpec) field.ErrorList {
	var allErrs field.ErrorList

	for key, tenant := range tenants {
		basePath := field.NewPath("spec", "limits", "tenants").Key(key)
		allErrs = append(allErrs, v.validateOTLPSpec(basePath, tenant.OTLP)...)
	}

	return allErrs
}

func (v *LokiStackValidator) validateOTLPSpec(parent *field.Path, s *lokiv1.OTLPSpec) field.ErrorList {
	var allErrs field.ErrorList

	if s.ResourceAttributes != nil && s.ResourceAttributes.IgnoreDefaults {
		switch {
		case len(s.ResourceAttributes.Attributes) == 0:
			allErrs = append(allErrs,
				field.Invalid(
					parent.Child("otlp", "resourceAttributes"),
					[]lokiv1.OTLPAttributesSpec{},
					lokiv1.ErrOTLPResourceAttributesEmptyNotAllowed.Error(),
				),
			)
		default:
			var indexLabelActionFound bool
			for _, attr := range s.ResourceAttributes.Attributes {
				if attr.Action == lokiv1.OTLPAttributeActionIndexLabel {
					indexLabelActionFound = true
					break
				}
			}

			if !indexLabelActionFound {
				allErrs = append(allErrs,
					field.Invalid(
						parent.Child("otlp", "resourceAttributes"),
						s.ResourceAttributes.Attributes,
						lokiv1.ErrOTLPResourceAttributesIndexLabelActionMissing.Error(),
					),
				)
			}

			for idx, attr := range s.ResourceAttributes.Attributes {
				if len(attr.Attributes) == 0 && attr.Regex == "" {
					allErrs = append(allErrs,
						field.Invalid(
							parent.Child("otlp", "resourceAttributes").Index(idx),
							[]string{},
							lokiv1.ErrOTLPAttributesSpecInvalid.Error(),
						),
					)
				}
			}
		}
	}

	if len(s.ScopeAttributes) != 0 {
		for idx, attr := range s.ScopeAttributes {
			if len(attr.Attributes) == 0 && attr.Regex == "" {
				allErrs = append(allErrs,
					field.Invalid(
						parent.Child("otlp", "scopeAttributes").Index(idx),
						[]string{},
						lokiv1.ErrOTLPAttributesSpecInvalid.Error(),
					),
				)
			}
		}
	}

	if len(s.LogAttributes) != 0 {
		for idx, attr := range s.LogAttributes {
			if len(attr.Attributes) == 0 && attr.Regex == "" {
				allErrs = append(allErrs,
					field.Invalid(
						parent.Child("otlp", "logAttributes").Index(idx),
						[]string{},
						lokiv1.ErrOTLPAttributesSpecInvalid.Error(),
					),
				)
			}
		}
	}

	return allErrs
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
