// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[DashboardRegexMapOptions] = (*DashboardRegexMapOptionsBuilder)(nil)

type DashboardRegexMapOptionsBuilder struct {
	internal *DashboardRegexMapOptions
	errors   map[string]cog.BuildErrors
}

func NewDashboardRegexMapOptionsBuilder() *DashboardRegexMapOptionsBuilder {
	resource := &DashboardRegexMapOptions{}
	builder := &DashboardRegexMapOptionsBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *DashboardRegexMapOptionsBuilder) Build() (DashboardRegexMapOptions, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("DashboardRegexMapOptions", err)...)
	}

	if len(errs) != 0 {
		return DashboardRegexMapOptions{}, errs
	}

	return *builder.internal, nil
}

// Regular expression to match against
func (builder *DashboardRegexMapOptionsBuilder) Pattern(pattern string) *DashboardRegexMapOptionsBuilder {
	builder.internal.Pattern = pattern

	return builder
}

// Config to apply when the value matches the regex
func (builder *DashboardRegexMapOptionsBuilder) Result(result cog.Builder[ValueMappingResult]) *DashboardRegexMapOptionsBuilder {
	resultResource, err := result.Build()
	if err != nil {
		builder.errors["result"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Result = resultResource

	return builder
}

func (builder *DashboardRegexMapOptionsBuilder) applyDefaults() {
}
