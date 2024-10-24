// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[AnnotationActions] = (*AnnotationActionsBuilder)(nil)

type AnnotationActionsBuilder struct {
	internal *AnnotationActions
	errors   map[string]cog.BuildErrors
}

func NewAnnotationActionsBuilder() *AnnotationActionsBuilder {
	resource := &AnnotationActions{}
	builder := &AnnotationActionsBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *AnnotationActionsBuilder) Build() (AnnotationActions, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("AnnotationActions", err)...)
	}

	if len(errs) != 0 {
		return AnnotationActions{}, errs
	}

	return *builder.internal, nil
}

func (builder *AnnotationActionsBuilder) CanAdd(canAdd bool) *AnnotationActionsBuilder {
	builder.internal.CanAdd = &canAdd

	return builder
}

func (builder *AnnotationActionsBuilder) CanDelete(canDelete bool) *AnnotationActionsBuilder {
	builder.internal.CanDelete = &canDelete

	return builder
}

func (builder *AnnotationActionsBuilder) CanEdit(canEdit bool) *AnnotationActionsBuilder {
	builder.internal.CanEdit = &canEdit

	return builder
}

func (builder *AnnotationActionsBuilder) applyDefaults() {
}
