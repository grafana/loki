// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package testdata

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[USAQuery] = (*USAQueryBuilder)(nil)

type USAQueryBuilder struct {
	internal *USAQuery
	errors   map[string]cog.BuildErrors
}

func NewUSAQueryBuilder() *USAQueryBuilder {
	resource := &USAQuery{}
	builder := &USAQueryBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *USAQueryBuilder) Build() (USAQuery, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("USAQuery", err)...)
	}

	if len(errs) != 0 {
		return USAQuery{}, errs
	}

	return *builder.internal, nil
}

func (builder *USAQueryBuilder) Fields(fields []string) *USAQueryBuilder {
	builder.internal.Fields = fields

	return builder
}

func (builder *USAQueryBuilder) Mode(mode string) *USAQueryBuilder {
	builder.internal.Mode = &mode

	return builder
}

func (builder *USAQueryBuilder) Period(period string) *USAQueryBuilder {
	builder.internal.Period = &period

	return builder
}

func (builder *USAQueryBuilder) States(states []string) *USAQueryBuilder {
	builder.internal.States = states

	return builder
}

func (builder *USAQueryBuilder) applyDefaults() {
}
