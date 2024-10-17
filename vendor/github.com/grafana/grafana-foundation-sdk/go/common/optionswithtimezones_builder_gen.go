// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[OptionsWithTimezones] = (*OptionsWithTimezonesBuilder)(nil)

// TODO docs
type OptionsWithTimezonesBuilder struct {
	internal *OptionsWithTimezones
	errors   map[string]cog.BuildErrors
}

func NewOptionsWithTimezonesBuilder() *OptionsWithTimezonesBuilder {
	resource := &OptionsWithTimezones{}
	builder := &OptionsWithTimezonesBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *OptionsWithTimezonesBuilder) Build() (OptionsWithTimezones, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("OptionsWithTimezones", err)...)
	}

	if len(errs) != 0 {
		return OptionsWithTimezones{}, errs
	}

	return *builder.internal, nil
}

func (builder *OptionsWithTimezonesBuilder) Timezone(timezone []TimeZone) *OptionsWithTimezonesBuilder {
	builder.internal.Timezone = timezone

	return builder
}

func (builder *OptionsWithTimezonesBuilder) applyDefaults() {
}
