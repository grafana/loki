// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[DashboardDashboardTemplating] = (*DashboardDashboardTemplatingBuilder)(nil)

type DashboardDashboardTemplatingBuilder struct {
	internal *DashboardDashboardTemplating
	errors   map[string]cog.BuildErrors
}

func NewDashboardDashboardTemplatingBuilder() *DashboardDashboardTemplatingBuilder {
	resource := &DashboardDashboardTemplating{}
	builder := &DashboardDashboardTemplatingBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *DashboardDashboardTemplatingBuilder) Build() (DashboardDashboardTemplating, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("DashboardDashboardTemplating", err)...)
	}

	if len(errs) != 0 {
		return DashboardDashboardTemplating{}, errs
	}

	return *builder.internal, nil
}

// List of configured template variables with their saved values along with some other metadata
func (builder *DashboardDashboardTemplatingBuilder) List(list []cog.Builder[VariableModel]) *DashboardDashboardTemplatingBuilder {
	listResources := make([]VariableModel, 0, len(list))
	for _, r1 := range list {
		listDepth1, err := r1.Build()
		if err != nil {
			builder.errors["list"] = err.(cog.BuildErrors)
			return builder
		}
		listResources = append(listResources, listDepth1)
	}
	builder.internal.List = listResources

	return builder
}

func (builder *DashboardDashboardTemplatingBuilder) applyDefaults() {
}
