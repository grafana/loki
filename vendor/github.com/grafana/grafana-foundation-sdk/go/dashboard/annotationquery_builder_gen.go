// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[AnnotationQuery] = (*AnnotationQueryBuilder)(nil)

// TODO docs
// FROM: AnnotationQuery in grafana-data/src/types/annotations.ts
type AnnotationQueryBuilder struct {
	internal *AnnotationQuery
	errors   map[string]cog.BuildErrors
}

func NewAnnotationQueryBuilder() *AnnotationQueryBuilder {
	resource := &AnnotationQuery{}
	builder := &AnnotationQueryBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *AnnotationQueryBuilder) Build() (AnnotationQuery, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("AnnotationQuery", err)...)
	}

	if len(errs) != 0 {
		return AnnotationQuery{}, errs
	}

	return *builder.internal, nil
}

// Name of annotation.
func (builder *AnnotationQueryBuilder) Name(name string) *AnnotationQueryBuilder {
	builder.internal.Name = name

	return builder
}

// Datasource where the annotations data is
func (builder *AnnotationQueryBuilder) Datasource(datasource DataSourceRef) *AnnotationQueryBuilder {
	builder.internal.Datasource = datasource

	return builder
}

// When enabled the annotation query is issued with every dashboard refresh
func (builder *AnnotationQueryBuilder) Enable(enable bool) *AnnotationQueryBuilder {
	builder.internal.Enable = enable

	return builder
}

// Annotation queries can be toggled on or off at the top of the dashboard.
// When hide is true, the toggle is not shown in the dashboard.
func (builder *AnnotationQueryBuilder) Hide(hide bool) *AnnotationQueryBuilder {
	builder.internal.Hide = &hide

	return builder
}

// Color to use for the annotation event markers
func (builder *AnnotationQueryBuilder) IconColor(iconColor string) *AnnotationQueryBuilder {
	builder.internal.IconColor = iconColor

	return builder
}

// Filters to apply when fetching annotations
func (builder *AnnotationQueryBuilder) Filter(filter cog.Builder[AnnotationPanelFilter]) *AnnotationQueryBuilder {
	filterResource, err := filter.Build()
	if err != nil {
		builder.errors["filter"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Filter = &filterResource

	return builder
}

// TODO.. this should just be a normal query target
func (builder *AnnotationQueryBuilder) Target(target cog.Builder[AnnotationTarget]) *AnnotationQueryBuilder {
	targetResource, err := target.Build()
	if err != nil {
		builder.errors["target"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Target = &targetResource

	return builder
}

// TODO -- this should not exist here, it is based on the --grafana-- datasource
func (builder *AnnotationQueryBuilder) Type(typeArg string) *AnnotationQueryBuilder {
	builder.internal.Type = &typeArg

	return builder
}

// Set to 1 for the standard annotation query all dashboards have by default.
func (builder *AnnotationQueryBuilder) BuiltIn(builtIn float64) *AnnotationQueryBuilder {
	builder.internal.BuiltIn = &builtIn

	return builder
}

func (builder *AnnotationQueryBuilder) Expr(expr string) *AnnotationQueryBuilder {
	builder.internal.Expr = &expr

	return builder
}

func (builder *AnnotationQueryBuilder) applyDefaults() {
	builder.Enable(true)
	builder.Hide(false)
	builder.BuiltIn(0)
}
