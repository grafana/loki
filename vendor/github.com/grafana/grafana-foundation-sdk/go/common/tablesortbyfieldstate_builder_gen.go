// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[TableSortByFieldState] = (*TableSortByFieldStateBuilder)(nil)

// Sort by field state
type TableSortByFieldStateBuilder struct {
	internal *TableSortByFieldState
	errors   map[string]cog.BuildErrors
}

func NewTableSortByFieldStateBuilder() *TableSortByFieldStateBuilder {
	resource := &TableSortByFieldState{}
	builder := &TableSortByFieldStateBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *TableSortByFieldStateBuilder) Build() (TableSortByFieldState, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("TableSortByFieldState", err)...)
	}

	if len(errs) != 0 {
		return TableSortByFieldState{}, errs
	}

	return *builder.internal, nil
}

// Sets the display name of the field to sort by
func (builder *TableSortByFieldStateBuilder) DisplayName(displayName string) *TableSortByFieldStateBuilder {
	builder.internal.DisplayName = displayName

	return builder
}

// Flag used to indicate descending sort order
func (builder *TableSortByFieldStateBuilder) Desc(desc bool) *TableSortByFieldStateBuilder {
	builder.internal.Desc = &desc

	return builder
}

func (builder *TableSortByFieldStateBuilder) applyDefaults() {
}
