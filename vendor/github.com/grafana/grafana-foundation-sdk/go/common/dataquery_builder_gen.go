// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[DataQuery] = (*DataQueryBuilder)(nil)

// These are the common properties available to all queries in all datasources.
// Specific implementations will *extend* this interface, adding the required
// properties for the given context.
type DataQueryBuilder struct {
	internal *DataQuery
	errors   map[string]cog.BuildErrors
}

func NewDataQueryBuilder() *DataQueryBuilder {
	resource := &DataQuery{}
	builder := &DataQueryBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *DataQueryBuilder) Build() (DataQuery, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("DataQuery", err)...)
	}

	if len(errs) != 0 {
		return DataQuery{}, errs
	}

	return *builder.internal, nil
}

// A unique identifier for the query within the list of targets.
// In server side expressions, the refId is used as a variable name to identify results.
// By default, the UI will assign A->Z; however setting meaningful names may be useful.
func (builder *DataQueryBuilder) RefId(refId string) *DataQueryBuilder {
	builder.internal.RefId = refId

	return builder
}

// If hide is set to true, Grafana will filter out the response(s) associated with this query before returning it to the panel.
func (builder *DataQueryBuilder) Hide(hide bool) *DataQueryBuilder {
	builder.internal.Hide = &hide

	return builder
}

// Specify the query flavor
// TODO make this required and give it a default
func (builder *DataQueryBuilder) QueryType(queryType string) *DataQueryBuilder {
	builder.internal.QueryType = &queryType

	return builder
}

// For mixed data sources the selected datasource is on the query level.
// For non mixed scenarios this is undefined.
// TODO find a better way to do this ^ that's friendly to schema
// TODO this shouldn't be unknown but DataSourceRef | null
func (builder *DataQueryBuilder) Datasource(datasource any) *DataQueryBuilder {
	builder.internal.Datasource = &datasource

	return builder
}

func (builder *DataQueryBuilder) applyDefaults() {
}
