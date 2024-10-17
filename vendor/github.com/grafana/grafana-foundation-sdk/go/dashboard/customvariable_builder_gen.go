// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[VariableModel] = (*CustomVariableBuilder)(nil)

// A variable is a placeholder for a value. You can use variables in metric queries and in panel titles.
type CustomVariableBuilder struct {
	internal *VariableModel
	errors   map[string]cog.BuildErrors
}

func NewCustomVariableBuilder(name string) *CustomVariableBuilder {
	resource := &VariableModel{}
	builder := &CustomVariableBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()
	builder.internal.Name = name
	builder.internal.Type = "custom"

	return builder
}

func (builder *CustomVariableBuilder) Build() (VariableModel, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("CustomVariable", err)...)
	}

	if len(errs) != 0 {
		return VariableModel{}, errs
	}

	return *builder.internal, nil
}

// Name of variable
func (builder *CustomVariableBuilder) Name(name string) *CustomVariableBuilder {
	builder.internal.Name = name

	return builder
}

// Optional display name
func (builder *CustomVariableBuilder) Label(label string) *CustomVariableBuilder {
	builder.internal.Label = &label

	return builder
}

// Visibility configuration for the variable
func (builder *CustomVariableBuilder) Hide(hide VariableHide) *CustomVariableBuilder {
	builder.internal.Hide = &hide

	return builder
}

// Description of variable. It can be defined but `null`.
func (builder *CustomVariableBuilder) Description(description string) *CustomVariableBuilder {
	builder.internal.Description = &description

	return builder
}

// Query used to fetch values for a variable
func (builder *CustomVariableBuilder) Values(query StringOrMap) *CustomVariableBuilder {
	builder.internal.Query = &query

	return builder
}

// Shows current selected variable text/value on the dashboard
func (builder *CustomVariableBuilder) Current(current VariableOption) *CustomVariableBuilder {
	builder.internal.Current = &current

	return builder
}

// Whether multiple values can be selected or not from variable value list
func (builder *CustomVariableBuilder) Multi(multi bool) *CustomVariableBuilder {
	builder.internal.Multi = &multi

	return builder
}

// Options that can be selected for a variable.
func (builder *CustomVariableBuilder) Options(options []VariableOption) *CustomVariableBuilder {
	builder.internal.Options = options

	return builder
}

// Whether all value option is available or not
func (builder *CustomVariableBuilder) IncludeAll(includeAll bool) *CustomVariableBuilder {
	builder.internal.IncludeAll = &includeAll

	return builder
}

// Custom all value
func (builder *CustomVariableBuilder) AllValue(allValue string) *CustomVariableBuilder {
	builder.internal.AllValue = &allValue

	return builder
}

func (builder *CustomVariableBuilder) applyDefaults() {
}
