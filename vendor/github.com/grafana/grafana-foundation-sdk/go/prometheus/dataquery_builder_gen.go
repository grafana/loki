// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package prometheus

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
	variants "github.com/grafana/grafana-foundation-sdk/go/cog/variants"
	dashboard "github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

var _ cog.Builder[variants.Dataquery] = (*DataqueryBuilder)(nil)

type DataqueryBuilder struct {
	internal *Dataquery
	errors   map[string]cog.BuildErrors
}

func NewDataqueryBuilder() *DataqueryBuilder {
	resource := &Dataquery{}
	builder := &DataqueryBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *DataqueryBuilder) Build() (variants.Dataquery, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("Dataquery", err)...)
	}

	if len(errs) != 0 {
		return Dataquery{}, errs
	}

	return *builder.internal, nil
}

// The actual expression/query that will be evaluated by Prometheus
func (builder *DataqueryBuilder) Expr(expr string) *DataqueryBuilder {
	builder.internal.Expr = expr

	return builder
}

// Returns only the latest value that Prometheus has scraped for the requested time series
func (builder *DataqueryBuilder) Instant() *DataqueryBuilder {
	valInstant := true
	builder.internal.Instant = &valInstant
	valRange := false
	builder.internal.Range = &valRange

	return builder
}

// Returns a Range vector, comprised of a set of time series containing a range of data points over time for each time series
func (builder *DataqueryBuilder) Range() *DataqueryBuilder {
	valRange := true
	builder.internal.Range = &valRange
	valInstant := false
	builder.internal.Instant = &valInstant

	return builder
}

// Execute an additional query to identify interesting raw samples relevant for the given expr
func (builder *DataqueryBuilder) Exemplar(exemplar bool) *DataqueryBuilder {
	builder.internal.Exemplar = &exemplar

	return builder
}

// Specifies which editor is being used to prepare the query. It can be "code" or "builder"
func (builder *DataqueryBuilder) EditorMode(editorMode QueryEditorMode) *DataqueryBuilder {
	builder.internal.EditorMode = &editorMode

	return builder
}

// Query format to determine how to display data points in panel. It can be "time_series", "table", "heatmap"
func (builder *DataqueryBuilder) Format(format PromQueryFormat) *DataqueryBuilder {
	builder.internal.Format = &format

	return builder
}

// Series name override or template. Ex. {{hostname}} will be replaced with label value for hostname
func (builder *DataqueryBuilder) LegendFormat(legendFormat string) *DataqueryBuilder {
	builder.internal.LegendFormat = &legendFormat

	return builder
}

// @deprecated Used to specify how many times to divide max data points by. We use max data points under query options
// See https://github.com/grafana/grafana/issues/48081
func (builder *DataqueryBuilder) IntervalFactor(intervalFactor float64) *DataqueryBuilder {
	builder.internal.IntervalFactor = &intervalFactor

	return builder
}

// A unique identifier for the query within the list of targets.
// In server side expressions, the refId is used as a variable name to identify results.
// By default, the UI will assign A->Z; however setting meaningful names may be useful.
func (builder *DataqueryBuilder) RefId(refId string) *DataqueryBuilder {
	builder.internal.RefId = refId

	return builder
}

// If hide is set to true, Grafana will filter out the response(s) associated with this query before returning it to the panel.
func (builder *DataqueryBuilder) Hide(hide bool) *DataqueryBuilder {
	builder.internal.Hide = &hide

	return builder
}

// Specify the query flavor
// TODO make this required and give it a default
func (builder *DataqueryBuilder) QueryType(queryType string) *DataqueryBuilder {
	builder.internal.QueryType = &queryType

	return builder
}

// For mixed data sources the selected datasource is on the query level.
// For non mixed scenarios this is undefined.
// TODO find a better way to do this ^ that's friendly to schema
// TODO this shouldn't be unknown but DataSourceRef | null
func (builder *DataqueryBuilder) Datasource(datasource dashboard.DataSourceRef) *DataqueryBuilder {
	builder.internal.Datasource = &datasource

	return builder
}

// An additional lower limit for the step parameter of the Prometheus query and for the
// `$__interval` and `$__rate_interval` variables.
func (builder *DataqueryBuilder) Interval(interval string) *DataqueryBuilder {
	builder.internal.Interval = &interval

	return builder
}

func (builder *DataqueryBuilder) RangeAndInstant() *DataqueryBuilder {
	valRange := true
	builder.internal.Range = &valRange
	valInstant := true
	builder.internal.Instant = &valInstant

	return builder
}

func (builder *DataqueryBuilder) applyDefaults() {
}
