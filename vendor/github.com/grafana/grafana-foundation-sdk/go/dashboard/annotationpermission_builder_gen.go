// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[AnnotationPermission] = (*AnnotationPermissionBuilder)(nil)

type AnnotationPermissionBuilder struct {
	internal *AnnotationPermission
	errors   map[string]cog.BuildErrors
}

func NewAnnotationPermissionBuilder() *AnnotationPermissionBuilder {
	resource := &AnnotationPermission{}
	builder := &AnnotationPermissionBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *AnnotationPermissionBuilder) Build() (AnnotationPermission, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("AnnotationPermission", err)...)
	}

	if len(errs) != 0 {
		return AnnotationPermission{}, errs
	}

	return *builder.internal, nil
}

func (builder *AnnotationPermissionBuilder) Dashboard(dashboard cog.Builder[AnnotationActions]) *AnnotationPermissionBuilder {
	dashboardResource, err := dashboard.Build()
	if err != nil {
		builder.errors["dashboard"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Dashboard = &dashboardResource

	return builder
}

func (builder *AnnotationPermissionBuilder) Organization(organization cog.Builder[AnnotationActions]) *AnnotationPermissionBuilder {
	organizationResource, err := organization.Build()
	if err != nil {
		builder.errors["organization"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Organization = &organizationResource

	return builder
}

func (builder *AnnotationPermissionBuilder) applyDefaults() {
}
