// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package testdata

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

func (builder *DataqueryBuilder) Alias(alias string) *DataqueryBuilder {
	builder.internal.Alias = &alias

	return builder
}

// Used for live query
func (builder *DataqueryBuilder) Channel(channel string) *DataqueryBuilder {
	builder.internal.Channel = &channel

	return builder
}

func (builder *DataqueryBuilder) CsvContent(csvContent string) *DataqueryBuilder {
	builder.internal.CsvContent = &csvContent

	return builder
}

func (builder *DataqueryBuilder) CsvFileName(csvFileName string) *DataqueryBuilder {
	builder.internal.CsvFileName = &csvFileName

	return builder
}

func (builder *DataqueryBuilder) CsvWave(csvWave []cog.Builder[CSVWave]) *DataqueryBuilder {
	csvWaveResources := make([]CSVWave, 0, len(csvWave))
	for _, r1 := range csvWave {
		csvWaveDepth1, err := r1.Build()
		if err != nil {
			builder.errors["csvWave"] = err.(cog.BuildErrors)
			return builder
		}
		csvWaveResources = append(csvWaveResources, csvWaveDepth1)
	}
	builder.internal.CsvWave = csvWaveResources

	return builder
}

// The datasource
func (builder *DataqueryBuilder) Datasource(datasource dashboard.DataSourceRef) *DataqueryBuilder {
	builder.internal.Datasource = &datasource

	return builder
}

// Drop percentage (the chance we will lose a point 0-100)
func (builder *DataqueryBuilder) DropPercent(dropPercent float64) *DataqueryBuilder {
	builder.internal.DropPercent = &dropPercent

	return builder
}

// Possible enum values:
//   - `"frontend_exception"`
//   - `"frontend_observable"`
//   - `"server_panic"`
func (builder *DataqueryBuilder) ErrorType(errorType DataqueryErrorType) *DataqueryBuilder {
	builder.internal.ErrorType = &errorType

	return builder
}

func (builder *DataqueryBuilder) FlamegraphDiff(flamegraphDiff bool) *DataqueryBuilder {
	builder.internal.FlamegraphDiff = &flamegraphDiff

	return builder
}

// true if query is disabled (ie should not be returned to the dashboard)
// NOTE: this does not always imply that the query should not be executed since
// the results from a hidden query may be used as the input to other queries (SSE etc)
func (builder *DataqueryBuilder) Hide(hide bool) *DataqueryBuilder {
	builder.internal.Hide = &hide

	return builder
}

// Interval is the suggested duration between time points in a time series query.
// NOTE: the values for intervalMs is not saved in the query model.  It is typically calculated
// from the interval required to fill a pixels in the visualization
func (builder *DataqueryBuilder) IntervalMs(intervalMs float64) *DataqueryBuilder {
	builder.internal.IntervalMs = &intervalMs

	return builder
}

func (builder *DataqueryBuilder) Labels(labels string) *DataqueryBuilder {
	builder.internal.Labels = &labels

	return builder
}

func (builder *DataqueryBuilder) LevelColumn(levelColumn bool) *DataqueryBuilder {
	builder.internal.LevelColumn = &levelColumn

	return builder
}

func (builder *DataqueryBuilder) Lines(lines int64) *DataqueryBuilder {
	builder.internal.Lines = &lines

	return builder
}

func (builder *DataqueryBuilder) Max(max float64) *DataqueryBuilder {
	builder.internal.Max = &max

	return builder
}

// MaxDataPoints is the maximum number of data points that should be returned from a time series query.
// NOTE: the values for maxDataPoints is not saved in the query model.  It is typically calculated
// from the number of pixels visible in a visualization
func (builder *DataqueryBuilder) MaxDataPoints(maxDataPoints int64) *DataqueryBuilder {
	builder.internal.MaxDataPoints = &maxDataPoints

	return builder
}

func (builder *DataqueryBuilder) Min(min float64) *DataqueryBuilder {
	builder.internal.Min = &min

	return builder
}

func (builder *DataqueryBuilder) Nodes(nodes cog.Builder[NodesQuery]) *DataqueryBuilder {
	nodesResource, err := nodes.Build()
	if err != nil {
		builder.errors["nodes"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Nodes = &nodesResource

	return builder
}

func (builder *DataqueryBuilder) Noise(noise float64) *DataqueryBuilder {
	builder.internal.Noise = &noise

	return builder
}

func (builder *DataqueryBuilder) Points(points [][]any) *DataqueryBuilder {
	builder.internal.Points = points

	return builder
}

func (builder *DataqueryBuilder) PulseWave(pulseWave cog.Builder[PulseWaveQuery]) *DataqueryBuilder {
	pulseWaveResource, err := pulseWave.Build()
	if err != nil {
		builder.errors["pulseWave"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.PulseWave = &pulseWaveResource

	return builder
}

// QueryType is an optional identifier for the type of query.
// It can be used to distinguish different types of queries.
func (builder *DataqueryBuilder) QueryType(queryType string) *DataqueryBuilder {
	builder.internal.QueryType = &queryType

	return builder
}

func (builder *DataqueryBuilder) RawFrameContent(rawFrameContent string) *DataqueryBuilder {
	builder.internal.RawFrameContent = &rawFrameContent

	return builder
}

// RefID is the unique identifier of the query, set by the frontend call.
func (builder *DataqueryBuilder) RefId(refId string) *DataqueryBuilder {
	builder.internal.RefId = &refId

	return builder
}

// Optionally define expected query result behavior
func (builder *DataqueryBuilder) ResultAssertions(resultAssertions cog.Builder[ResultAssertions]) *DataqueryBuilder {
	resultAssertionsResource, err := resultAssertions.Build()
	if err != nil {
		builder.errors["resultAssertions"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.ResultAssertions = &resultAssertionsResource

	return builder
}

// Possible enum values:
//   - `"annotations"`
//   - `"arrow"`
//   - `"csv_content"`
//   - `"csv_file"`
//   - `"csv_metric_values"`
//   - `"datapoints_outside_range"`
//   - `"exponential_heatmap_bucket_data"`
//   - `"flame_graph"`
//   - `"grafana_api"`
//   - `"linear_heatmap_bucket_data"`
//   - `"live"`
//   - `"logs"`
//   - `"manual_entry"`
//   - `"no_data_points"`
//   - `"node_graph"`
//   - `"predictable_csv_wave"`
//   - `"predictable_pulse"`
//   - `"random_walk"`
//   - `"random_walk_table"`
//   - `"random_walk_with_error"`
//   - `"raw_frame"`
//   - `"server_error_500"`
//   - `"simulation"`
//   - `"slow_query"`
//   - `"streaming_client"`
//   - `"table_static"`
//   - `"trace"`
//   - `"usa"`
//   - `"variables-query"`
func (builder *DataqueryBuilder) ScenarioId(scenarioId DataqueryScenarioId) *DataqueryBuilder {
	builder.internal.ScenarioId = &scenarioId

	return builder
}

func (builder *DataqueryBuilder) SeriesCount(seriesCount int64) *DataqueryBuilder {
	builder.internal.SeriesCount = &seriesCount

	return builder
}

func (builder *DataqueryBuilder) Sim(sim cog.Builder[SimulationQuery]) *DataqueryBuilder {
	simResource, err := sim.Build()
	if err != nil {
		builder.errors["sim"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Sim = &simResource

	return builder
}

func (builder *DataqueryBuilder) SpanCount(spanCount int64) *DataqueryBuilder {
	builder.internal.SpanCount = &spanCount

	return builder
}

func (builder *DataqueryBuilder) Spread(spread float64) *DataqueryBuilder {
	builder.internal.Spread = &spread

	return builder
}

func (builder *DataqueryBuilder) StartValue(startValue float64) *DataqueryBuilder {
	builder.internal.StartValue = &startValue

	return builder
}

func (builder *DataqueryBuilder) Stream(stream cog.Builder[StreamingQuery]) *DataqueryBuilder {
	streamResource, err := stream.Build()
	if err != nil {
		builder.errors["stream"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Stream = &streamResource

	return builder
}

// common parameter used by many query types
func (builder *DataqueryBuilder) StringInput(stringInput string) *DataqueryBuilder {
	builder.internal.StringInput = &stringInput

	return builder
}

// TimeRange represents the query range
// NOTE: unlike generic /ds/query, we can now send explicit time values in each query
// NOTE: the values for timeRange are not saved in a dashboard, they are constructed on the fly
func (builder *DataqueryBuilder) TimeRange(timeRange cog.Builder[TimeRange]) *DataqueryBuilder {
	timeRangeResource, err := timeRange.Build()
	if err != nil {
		builder.errors["timeRange"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.TimeRange = &timeRangeResource

	return builder
}

func (builder *DataqueryBuilder) Usa(usa cog.Builder[USAQuery]) *DataqueryBuilder {
	usaResource, err := usa.Build()
	if err != nil {
		builder.errors["usa"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Usa = &usaResource

	return builder
}

func (builder *DataqueryBuilder) WithNil(withNil bool) *DataqueryBuilder {
	builder.internal.WithNil = &withNil

	return builder
}

func (builder *DataqueryBuilder) applyDefaults() {
}
