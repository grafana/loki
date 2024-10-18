// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[AnnotationTarget] = (*AnnotationTargetBuilder)(nil)

// TODO: this should be a regular DataQuery that depends on the selected dashboard
// these match the properties of the "grafana" datasouce that is default in most dashboards
type AnnotationTargetBuilder struct {
	internal *AnnotationTarget
	errors   map[string]cog.BuildErrors
}

func NewAnnotationTargetBuilder() *AnnotationTargetBuilder {
	resource := &AnnotationTarget{}
	builder := &AnnotationTargetBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *AnnotationTargetBuilder) Build() (AnnotationTarget, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("AnnotationTarget", err)...)
	}

	if len(errs) != 0 {
		return AnnotationTarget{}, errs
	}

	return *builder.internal, nil
}

// Only required/valid for the grafana datasource...
// but code+tests is already depending on it so hard to change
func (builder *AnnotationTargetBuilder) Limit(limit int64) *AnnotationTargetBuilder {
	builder.internal.Limit = limit

	return builder
}

// Only required/valid for the grafana datasource...
// but code+tests is already depending on it so hard to change
func (builder *AnnotationTargetBuilder) MatchAny(matchAny bool) *AnnotationTargetBuilder {
	builder.internal.MatchAny = matchAny

	return builder
}

// Only required/valid for the grafana datasource...
// but code+tests is already depending on it so hard to change
func (builder *AnnotationTargetBuilder) Tags(tags []string) *AnnotationTargetBuilder {
	builder.internal.Tags = tags

	return builder
}

// Only required/valid for the grafana datasource...
// but code+tests is already depending on it so hard to change
func (builder *AnnotationTargetBuilder) Type(typeArg string) *AnnotationTargetBuilder {
	builder.internal.Type = typeArg

	return builder
}

func (builder *AnnotationTargetBuilder) applyDefaults() {
}
