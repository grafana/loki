// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package testdata

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[SimulationQuery] = (*SimulationQueryBuilder)(nil)

type SimulationQueryBuilder struct {
	internal *SimulationQuery
	errors   map[string]cog.BuildErrors
}

func NewSimulationQueryBuilder() *SimulationQueryBuilder {
	resource := &SimulationQuery{}
	builder := &SimulationQueryBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *SimulationQueryBuilder) Build() (SimulationQuery, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("SimulationQuery", err)...)
	}

	if len(errs) != 0 {
		return SimulationQuery{}, errs
	}

	return *builder.internal, nil
}

func (builder *SimulationQueryBuilder) Config(config any) *SimulationQueryBuilder {
	builder.internal.Config = &config

	return builder
}

func (builder *SimulationQueryBuilder) Key(key cog.Builder[Key]) *SimulationQueryBuilder {
	keyResource, err := key.Build()
	if err != nil {
		builder.errors["key"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Key = keyResource

	return builder
}

func (builder *SimulationQueryBuilder) Last(last bool) *SimulationQueryBuilder {
	builder.internal.Last = &last

	return builder
}

func (builder *SimulationQueryBuilder) Stream(stream bool) *SimulationQueryBuilder {
	builder.internal.Stream = &stream

	return builder
}

func (builder *SimulationQueryBuilder) applyDefaults() {
}
