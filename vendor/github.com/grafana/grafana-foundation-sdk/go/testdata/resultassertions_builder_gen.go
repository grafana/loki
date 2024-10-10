// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package testdata

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[ResultAssertions] = (*ResultAssertionsBuilder)(nil)

type ResultAssertionsBuilder struct {
	internal *ResultAssertions
	errors   map[string]cog.BuildErrors
}

func NewResultAssertionsBuilder() *ResultAssertionsBuilder {
	resource := &ResultAssertions{}
	builder := &ResultAssertionsBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *ResultAssertionsBuilder) Build() (ResultAssertions, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("ResultAssertions", err)...)
	}

	if len(errs) != 0 {
		return ResultAssertions{}, errs
	}

	return *builder.internal, nil
}

// Maximum frame count
func (builder *ResultAssertionsBuilder) MaxFrames(maxFrames int64) *ResultAssertionsBuilder {
	builder.internal.MaxFrames = &maxFrames

	return builder
}

// Type asserts that the frame matches a known type structure.
// Possible enum values:
//   - `""`
//   - `"timeseries-wide"`
//   - `"timeseries-long"`
//   - `"timeseries-many"`
//   - `"timeseries-multi"`
//   - `"directory-listing"`
//   - `"table"`
//   - `"numeric-wide"`
//   - `"numeric-multi"`
//   - `"numeric-long"`
//   - `"log-lines"`
func (builder *ResultAssertionsBuilder) Type(typeArg ResultAssertionsType) *ResultAssertionsBuilder {
	builder.internal.Type = &typeArg

	return builder
}

// TypeVersion is the version of the Type property. Versions greater than 0.0 correspond to the dataplane
// contract documentation https://grafana.github.io/dataplane/contract/.
func (builder *ResultAssertionsBuilder) TypeVersion(typeVersion []int64) *ResultAssertionsBuilder {
	builder.internal.TypeVersion = typeVersion

	return builder
}

func (builder *ResultAssertionsBuilder) applyDefaults() {
}
