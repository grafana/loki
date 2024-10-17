// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[SpecialValueMap] = (*SpecialValueMapBuilder)(nil)

// Maps special values like Null, NaN (not a number), and boolean values like true and false to a display text and color.
// See SpecialValueMatch to see the list of special values.
// For example, you can configure a special value mapping so that null values appear as N/A.
type SpecialValueMapBuilder struct {
	internal *SpecialValueMap
	errors   map[string]cog.BuildErrors
}

func NewSpecialValueMapBuilder() *SpecialValueMapBuilder {
	resource := &SpecialValueMap{}
	builder := &SpecialValueMapBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()
	builder.internal.Type = "special"

	return builder
}

func (builder *SpecialValueMapBuilder) Build() (SpecialValueMap, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("SpecialValueMap", err)...)
	}

	if len(errs) != 0 {
		return SpecialValueMap{}, errs
	}

	return *builder.internal, nil
}

func (builder *SpecialValueMapBuilder) Options(options cog.Builder[DashboardSpecialValueMapOptions]) *SpecialValueMapBuilder {
	optionsResource, err := options.Build()
	if err != nil {
		builder.errors["options"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Options = optionsResource

	return builder
}

func (builder *SpecialValueMapBuilder) applyDefaults() {
}
