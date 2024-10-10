// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package testdata

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[NodesQuery] = (*NodesQueryBuilder)(nil)

type NodesQueryBuilder struct {
	internal *NodesQuery
	errors   map[string]cog.BuildErrors
}

func NewNodesQueryBuilder() *NodesQueryBuilder {
	resource := &NodesQuery{}
	builder := &NodesQueryBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *NodesQueryBuilder) Build() (NodesQuery, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("NodesQuery", err)...)
	}

	if len(errs) != 0 {
		return NodesQuery{}, errs
	}

	return *builder.internal, nil
}

func (builder *NodesQueryBuilder) Count(count int64) *NodesQueryBuilder {
	builder.internal.Count = &count

	return builder
}

func (builder *NodesQueryBuilder) Seed(seed int64) *NodesQueryBuilder {
	builder.internal.Seed = &seed

	return builder
}

// Possible enum values:
//   - `"random"`
//   - `"random edges"`
//   - `"response_medium"`
//   - `"response_small"`
//   - `"feature_showcase"`
func (builder *NodesQueryBuilder) Type(typeArg NodesQueryType) *NodesQueryBuilder {
	builder.internal.Type = &typeArg

	return builder
}

func (builder *NodesQueryBuilder) applyDefaults() {
}
