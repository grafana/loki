package openshift

import (
	"k8s.io/apimachinery/pkg/util/validation/field"

	lokiv1 "github.com/grafana/loki/operator/api/loki/v1"
)

var (
	requiredStreamLabels = []string{
		"k8s.namespace.name",
		"kubernetes.namespace_name",
		"log_source",
		"log_type",
		"openshift.cluster.uid",
		"openshift.log.source",
		"openshift.log.type",
	}

	optionalStreamLabels = []string{
		"k8s.container.name",
		"k8s.cronjob.name",
		"k8s.daemonset.name",
		"k8s.deployment.name",
		"k8s.job.name",
		"k8s.node.name",
		"k8s.pod.name",
		"k8s.statefulset.name",
		"kubernetes.container_name",
		"kubernetes.host",
		"kubernetes.pod_name",
		"service.name",
	}
)

// ValidateOTLPInvalidDrop validates that a spec does not drop required OTLP attributes in the openshift-logging tenancy mode.
func ValidateOTLPInvalidDrop(spec *lokiv1.LokiStackSpec) field.ErrorList {
	if spec.Limits == nil {
		return nil
	}

	requiredAttributes := map[string]bool{}
	for _, label := range requiredStreamLabels {
		requiredAttributes[label] = true
	}

	disableRecommendedAttributes := false
	if spec.Tenants != nil && spec.Tenants.Openshift != nil && spec.Tenants.Openshift.OTLP != nil {
		disableRecommendedAttributes = spec.Tenants.Openshift.OTLP.DisableRecommendedAttributes
	}
	if !disableRecommendedAttributes {
		for _, label := range optionalStreamLabels {
			requiredAttributes[label] = true
		}
	}

	errList := field.ErrorList{}
	if spec.Limits.Global != nil && spec.Limits.Global.OTLP != nil {
		errList = append(errList, validateOTLPSpec(requiredAttributes, spec.Limits.Global.OTLP, field.NewPath("spec", "limits", "global", "otlp"))...)
	}

	if len(spec.Limits.Tenants) > 0 {
		for name, tenant := range spec.Limits.Tenants {
			if tenant.OTLP != nil {
				errList = append(errList, validateOTLPSpec(requiredAttributes, tenant.OTLP, field.NewPath("spec", "limits", "tenants", name, "otlp"))...)
			}
		}
	}

	return errList
}

func validateOTLPSpec(requiredAttributes map[string]bool, otlp *lokiv1.OTLPSpec, basePath *field.Path) field.ErrorList {
	if otlp.Drop == nil {
		return nil
	}

	errList := field.ErrorList{}
	for i, attr := range otlp.Drop.ResourceAttributes {
		if attr.Regex {
			continue
		}

		if !requiredAttributes[attr.Name] {
			continue
		}

		errList = append(errList, field.Invalid(
			basePath.Child("drop", "resourceAttributes").Index(i).Child("name"),
			attr.Name,
			lokiv1.ErrOTLPInvalidDrop.Error(),
		))
	}

	return errList
}
