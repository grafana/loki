// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[ValueMap] = (*ValueMapBuilder)(nil)

// Maps text values to a color or different display text and color.
// For example, you can configure a value mapping so that all instances of the value 10 appear as Perfection! rather than the number.
type ValueMapBuilder struct {
	internal *ValueMap
	errors   map[string]cog.BuildErrors
}

func NewValueMapBuilder() *ValueMapBuilder {
	resource := &ValueMap{}
	builder := &ValueMapBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()
	builder.internal.Type = "value"

	return builder
}

func (builder *ValueMapBuilder) Build() (ValueMap, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("ValueMap", err)...)
	}

	if len(errs) != 0 {
		return ValueMap{}, errs
	}

	return *builder.internal, nil
}

// Map with <value_to_match>: ValueMappingResult. For example: { "10": { text: "Perfection!", color: "green" } }
func (builder *ValueMapBuilder) Options(options map[string]ValueMappingResult) *ValueMapBuilder {
	builder.internal.Options = options

	return builder
}

func (builder *ValueMapBuilder) applyDefaults() {
}
