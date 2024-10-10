// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[VariableModel] = (*ConstantVariableBuilder)(nil)

// A variable is a placeholder for a value. You can use variables in metric queries and in panel titles.
type ConstantVariableBuilder struct {
	internal *VariableModel
	errors   map[string]cog.BuildErrors
}

func NewConstantVariableBuilder(name string) *ConstantVariableBuilder {
	resource := &VariableModel{}
	builder := &ConstantVariableBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()
	builder.internal.Name = name
	builder.internal.Type = "constant"

	return builder
}

func (builder *ConstantVariableBuilder) Build() (VariableModel, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("ConstantVariable", err)...)
	}

	if len(errs) != 0 {
		return VariableModel{}, errs
	}

	return *builder.internal, nil
}

// Name of variable
func (builder *ConstantVariableBuilder) Name(name string) *ConstantVariableBuilder {
	builder.internal.Name = name

	return builder
}

// Optional display name
func (builder *ConstantVariableBuilder) Label(label string) *ConstantVariableBuilder {
	builder.internal.Label = &label

	return builder
}

// Description of variable. It can be defined but `null`.
func (builder *ConstantVariableBuilder) Description(description string) *ConstantVariableBuilder {
	builder.internal.Description = &description

	return builder
}

// Query used to fetch values for a variable
func (builder *ConstantVariableBuilder) Value(query StringOrMap) *ConstantVariableBuilder {
	builder.internal.Query = &query

	return builder
}

func (builder *ConstantVariableBuilder) applyDefaults() {
}
