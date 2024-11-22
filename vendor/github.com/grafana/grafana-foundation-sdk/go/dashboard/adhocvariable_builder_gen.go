// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[VariableModel] = (*AdHocVariableBuilder)(nil)

// A variable is a placeholder for a value. You can use variables in metric queries and in panel titles.
type AdHocVariableBuilder struct {
	internal *VariableModel
	errors   map[string]cog.BuildErrors
}

func NewAdHocVariableBuilder(name string) *AdHocVariableBuilder {
	resource := &VariableModel{}
	builder := &AdHocVariableBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()
	builder.internal.Name = name
	builder.internal.Type = "adhoc"

	return builder
}

func (builder *AdHocVariableBuilder) Build() (VariableModel, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("AdHocVariable", err)...)
	}

	if len(errs) != 0 {
		return VariableModel{}, errs
	}

	return *builder.internal, nil
}

// Name of variable
func (builder *AdHocVariableBuilder) Name(name string) *AdHocVariableBuilder {
	builder.internal.Name = name

	return builder
}

// Optional display name
func (builder *AdHocVariableBuilder) Label(label string) *AdHocVariableBuilder {
	builder.internal.Label = &label

	return builder
}

// Visibility configuration for the variable
func (builder *AdHocVariableBuilder) Hide(hide VariableHide) *AdHocVariableBuilder {
	builder.internal.Hide = &hide

	return builder
}

// Description of variable. It can be defined but `null`.
func (builder *AdHocVariableBuilder) Description(description string) *AdHocVariableBuilder {
	builder.internal.Description = &description

	return builder
}

// Data source used to fetch values for a variable. It can be defined but `null`.
func (builder *AdHocVariableBuilder) Datasource(datasource DataSourceRef) *AdHocVariableBuilder {
	builder.internal.Datasource = &datasource

	return builder
}

func (builder *AdHocVariableBuilder) applyDefaults() {
}
