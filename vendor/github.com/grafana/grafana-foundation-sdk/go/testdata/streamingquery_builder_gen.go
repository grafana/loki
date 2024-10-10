// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package testdata

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[StreamingQuery] = (*StreamingQueryBuilder)(nil)

type StreamingQueryBuilder struct {
	internal *StreamingQuery
	errors   map[string]cog.BuildErrors
}

func NewStreamingQueryBuilder() *StreamingQueryBuilder {
	resource := &StreamingQuery{}
	builder := &StreamingQueryBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *StreamingQueryBuilder) Build() (StreamingQuery, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("StreamingQuery", err)...)
	}

	if len(errs) != 0 {
		return StreamingQuery{}, errs
	}

	return *builder.internal, nil
}

func (builder *StreamingQueryBuilder) Bands(bands int64) *StreamingQueryBuilder {
	builder.internal.Bands = &bands

	return builder
}

func (builder *StreamingQueryBuilder) Noise(noise float64) *StreamingQueryBuilder {
	builder.internal.Noise = noise

	return builder
}

func (builder *StreamingQueryBuilder) Speed(speed float64) *StreamingQueryBuilder {
	builder.internal.Speed = speed

	return builder
}

func (builder *StreamingQueryBuilder) Spread(spread float64) *StreamingQueryBuilder {
	builder.internal.Spread = spread

	return builder
}

// Possible enum values:
//   - `"fetch"`
//   - `"logs"`
//   - `"signal"`
//   - `"traces"`
func (builder *StreamingQueryBuilder) Type(typeArg StreamingQueryType) *StreamingQueryBuilder {
	builder.internal.Type = typeArg

	return builder
}

func (builder *StreamingQueryBuilder) Url(url string) *StreamingQueryBuilder {
	builder.internal.Url = &url

	return builder
}

func (builder *StreamingQueryBuilder) applyDefaults() {
}
