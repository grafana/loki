// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package testdata

import (
	"encoding/json"
	"reflect"

	variants "github.com/grafana/grafana-foundation-sdk/go/cog/variants"
	dashboard "github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

type CSVWave struct {
	Labels    *string `json:"labels,omitempty"`
	Name      *string `json:"name,omitempty"`
	TimeStep  *int64  `json:"timeStep,omitempty"`
	ValuesCSV *string `json:"valuesCSV,omitempty"`
}

func (resource CSVWave) Equals(other CSVWave) bool {
	if resource.Labels == nil && other.Labels != nil || resource.Labels != nil && other.Labels == nil {
		return false
	}

	if resource.Labels != nil {
		if *resource.Labels != *other.Labels {
			return false
		}
	}
	if resource.Name == nil && other.Name != nil || resource.Name != nil && other.Name == nil {
		return false
	}

	if resource.Name != nil {
		if *resource.Name != *other.Name {
			return false
		}
	}
	if resource.TimeStep == nil && other.TimeStep != nil || resource.TimeStep != nil && other.TimeStep == nil {
		return false
	}

	if resource.TimeStep != nil {
		if *resource.TimeStep != *other.TimeStep {
			return false
		}
	}
	if resource.ValuesCSV == nil && other.ValuesCSV != nil || resource.ValuesCSV != nil && other.ValuesCSV == nil {
		return false
	}

	if resource.ValuesCSV != nil {
		if *resource.ValuesCSV != *other.ValuesCSV {
			return false
		}
	}

	return true
}

type NodesQuery struct {
	Count *int64 `json:"count,omitempty"`
	Seed  *int64 `json:"seed,omitempty"`
	// Possible enum values:
	//  - `"random"`
	//  - `"random edges"`
	//  - `"response_medium"`
	//  - `"response_small"`
	//  - `"feature_showcase"`
	Type *NodesQueryType `json:"type,omitempty"`
}

func (resource NodesQuery) Equals(other NodesQuery) bool {
	if resource.Count == nil && other.Count != nil || resource.Count != nil && other.Count == nil {
		return false
	}

	if resource.Count != nil {
		if *resource.Count != *other.Count {
			return false
		}
	}
	if resource.Seed == nil && other.Seed != nil || resource.Seed != nil && other.Seed == nil {
		return false
	}

	if resource.Seed != nil {
		if *resource.Seed != *other.Seed {
			return false
		}
	}
	if resource.Type == nil && other.Type != nil || resource.Type != nil && other.Type == nil {
		return false
	}

	if resource.Type != nil {
		if *resource.Type != *other.Type {
			return false
		}
	}

	return true
}

type PulseWaveQuery struct {
	OffCount *int64   `json:"offCount,omitempty"`
	OffValue *float64 `json:"offValue,omitempty"`
	OnCount  *int64   `json:"onCount,omitempty"`
	OnValue  *float64 `json:"onValue,omitempty"`
	TimeStep *int64   `json:"timeStep,omitempty"`
}

func (resource PulseWaveQuery) Equals(other PulseWaveQuery) bool {
	if resource.OffCount == nil && other.OffCount != nil || resource.OffCount != nil && other.OffCount == nil {
		return false
	}

	if resource.OffCount != nil {
		if *resource.OffCount != *other.OffCount {
			return false
		}
	}
	if resource.OffValue == nil && other.OffValue != nil || resource.OffValue != nil && other.OffValue == nil {
		return false
	}

	if resource.OffValue != nil {
		if *resource.OffValue != *other.OffValue {
			return false
		}
	}
	if resource.OnCount == nil && other.OnCount != nil || resource.OnCount != nil && other.OnCount == nil {
		return false
	}

	if resource.OnCount != nil {
		if *resource.OnCount != *other.OnCount {
			return false
		}
	}
	if resource.OnValue == nil && other.OnValue != nil || resource.OnValue != nil && other.OnValue == nil {
		return false
	}

	if resource.OnValue != nil {
		if *resource.OnValue != *other.OnValue {
			return false
		}
	}
	if resource.TimeStep == nil && other.TimeStep != nil || resource.TimeStep != nil && other.TimeStep == nil {
		return false
	}

	if resource.TimeStep != nil {
		if *resource.TimeStep != *other.TimeStep {
			return false
		}
	}

	return true
}

type ResultAssertions struct {
	// Maximum frame count
	MaxFrames *int64 `json:"maxFrames,omitempty"`
	// Type asserts that the frame matches a known type structure.
	// Possible enum values:
	//  - `""`
	//  - `"timeseries-wide"`
	//  - `"timeseries-long"`
	//  - `"timeseries-many"`
	//  - `"timeseries-multi"`
	//  - `"directory-listing"`
	//  - `"table"`
	//  - `"numeric-wide"`
	//  - `"numeric-multi"`
	//  - `"numeric-long"`
	//  - `"log-lines"`
	Type *ResultAssertionsType `json:"type,omitempty"`
	// TypeVersion is the version of the Type property. Versions greater than 0.0 correspond to the dataplane
	// contract documentation https://grafana.github.io/dataplane/contract/.
	TypeVersion []int64 `json:"typeVersion"`
}

func (resource ResultAssertions) Equals(other ResultAssertions) bool {
	if resource.MaxFrames == nil && other.MaxFrames != nil || resource.MaxFrames != nil && other.MaxFrames == nil {
		return false
	}

	if resource.MaxFrames != nil {
		if *resource.MaxFrames != *other.MaxFrames {
			return false
		}
	}
	if resource.Type == nil && other.Type != nil || resource.Type != nil && other.Type == nil {
		return false
	}

	if resource.Type != nil {
		if *resource.Type != *other.Type {
			return false
		}
	}

	if len(resource.TypeVersion) != len(other.TypeVersion) {
		return false
	}

	for i1 := range resource.TypeVersion {
		if resource.TypeVersion[i1] != other.TypeVersion[i1] {
			return false
		}
	}

	return true
}

type Key struct {
	Tick float64 `json:"tick"`
	Type string  `json:"type"`
	Uid  *string `json:"uid,omitempty"`
}

func (resource Key) Equals(other Key) bool {
	if resource.Tick != other.Tick {
		return false
	}
	if resource.Type != other.Type {
		return false
	}
	if resource.Uid == nil && other.Uid != nil || resource.Uid != nil && other.Uid == nil {
		return false
	}

	if resource.Uid != nil {
		if *resource.Uid != *other.Uid {
			return false
		}
	}

	return true
}

type SimulationQuery struct {
	Config any   `json:"config,omitempty"`
	Key    Key   `json:"key"`
	Last   *bool `json:"last,omitempty"`
	Stream *bool `json:"stream,omitempty"`
}

func (resource SimulationQuery) Equals(other SimulationQuery) bool {
	// is DeepEqual good enough here?
	if !reflect.DeepEqual(resource.Config, other.Config) {
		return false
	}
	if !resource.Key.Equals(other.Key) {
		return false
	}
	if resource.Last == nil && other.Last != nil || resource.Last != nil && other.Last == nil {
		return false
	}

	if resource.Last != nil {
		if *resource.Last != *other.Last {
			return false
		}
	}
	if resource.Stream == nil && other.Stream != nil || resource.Stream != nil && other.Stream == nil {
		return false
	}

	if resource.Stream != nil {
		if *resource.Stream != *other.Stream {
			return false
		}
	}

	return true
}

type StreamingQuery struct {
	Bands  *int64  `json:"bands,omitempty"`
	Noise  float64 `json:"noise"`
	Speed  float64 `json:"speed"`
	Spread float64 `json:"spread"`
	// Possible enum values:
	//  - `"fetch"`
	//  - `"logs"`
	//  - `"signal"`
	//  - `"traces"`
	Type StreamingQueryType `json:"type"`
	Url  *string            `json:"url,omitempty"`
}

func (resource StreamingQuery) Equals(other StreamingQuery) bool {
	if resource.Bands == nil && other.Bands != nil || resource.Bands != nil && other.Bands == nil {
		return false
	}

	if resource.Bands != nil {
		if *resource.Bands != *other.Bands {
			return false
		}
	}
	if resource.Noise != other.Noise {
		return false
	}
	if resource.Speed != other.Speed {
		return false
	}
	if resource.Spread != other.Spread {
		return false
	}
	if resource.Type != other.Type {
		return false
	}
	if resource.Url == nil && other.Url != nil || resource.Url != nil && other.Url == nil {
		return false
	}

	if resource.Url != nil {
		if *resource.Url != *other.Url {
			return false
		}
	}

	return true
}

type TimeRange struct {
	// From is the start time of the query.
	From string `json:"from"`
	// To is the end time of the query.
	To string `json:"to"`
}

func (resource TimeRange) Equals(other TimeRange) bool {
	if resource.From != other.From {
		return false
	}
	if resource.To != other.To {
		return false
	}

	return true
}

type USAQuery struct {
	Fields []string `json:"fields,omitempty"`
	Mode   *string  `json:"mode,omitempty"`
	Period *string  `json:"period,omitempty"`
	States []string `json:"states,omitempty"`
}

func (resource USAQuery) Equals(other USAQuery) bool {

	if len(resource.Fields) != len(other.Fields) {
		return false
	}

	for i1 := range resource.Fields {
		if resource.Fields[i1] != other.Fields[i1] {
			return false
		}
	}
	if resource.Mode == nil && other.Mode != nil || resource.Mode != nil && other.Mode == nil {
		return false
	}

	if resource.Mode != nil {
		if *resource.Mode != *other.Mode {
			return false
		}
	}
	if resource.Period == nil && other.Period != nil || resource.Period != nil && other.Period == nil {
		return false
	}

	if resource.Period != nil {
		if *resource.Period != *other.Period {
			return false
		}
	}

	if len(resource.States) != len(other.States) {
		return false
	}

	for i1 := range resource.States {
		if resource.States[i1] != other.States[i1] {
			return false
		}
	}

	return true
}

type Dataquery struct {
	Alias *string `json:"alias,omitempty"`
	// Used for live query
	Channel     *string   `json:"channel,omitempty"`
	CsvContent  *string   `json:"csvContent,omitempty"`
	CsvFileName *string   `json:"csvFileName,omitempty"`
	CsvWave     []CSVWave `json:"csvWave,omitempty"`
	// The datasource
	Datasource *dashboard.DataSourceRef `json:"datasource,omitempty"`
	// Drop percentage (the chance we will lose a point 0-100)
	DropPercent *float64 `json:"dropPercent,omitempty"`
	// Possible enum values:
	//  - `"frontend_exception"`
	//  - `"frontend_observable"`
	//  - `"server_panic"`
	ErrorType      *DataqueryErrorType `json:"errorType,omitempty"`
	FlamegraphDiff *bool               `json:"flamegraphDiff,omitempty"`
	// true if query is disabled (ie should not be returned to the dashboard)
	// NOTE: this does not always imply that the query should not be executed since
	// the results from a hidden query may be used as the input to other queries (SSE etc)
	Hide *bool `json:"hide,omitempty"`
	// Interval is the suggested duration between time points in a time series query.
	// NOTE: the values for intervalMs is not saved in the query model.  It is typically calculated
	// from the interval required to fill a pixels in the visualization
	IntervalMs  *float64 `json:"intervalMs,omitempty"`
	Labels      *string  `json:"labels,omitempty"`
	LevelColumn *bool    `json:"levelColumn,omitempty"`
	Lines       *int64   `json:"lines,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	// MaxDataPoints is the maximum number of data points that should be returned from a time series query.
	// NOTE: the values for maxDataPoints is not saved in the query model.  It is typically calculated
	// from the number of pixels visible in a visualization
	MaxDataPoints *int64          `json:"maxDataPoints,omitempty"`
	Min           *float64        `json:"min,omitempty"`
	Nodes         *NodesQuery     `json:"nodes,omitempty"`
	Noise         *float64        `json:"noise,omitempty"`
	Points        [][]any         `json:"points,omitempty"`
	PulseWave     *PulseWaveQuery `json:"pulseWave,omitempty"`
	// QueryType is an optional identifier for the type of query.
	// It can be used to distinguish different types of queries.
	QueryType       *string `json:"queryType,omitempty"`
	RawFrameContent *string `json:"rawFrameContent,omitempty"`
	// RefID is the unique identifier of the query, set by the frontend call.
	RefId *string `json:"refId,omitempty"`
	// Optionally define expected query result behavior
	ResultAssertions *ResultAssertions `json:"resultAssertions,omitempty"`
	// Possible enum values:
	//  - `"annotations"`
	//  - `"arrow"`
	//  - `"csv_content"`
	//  - `"csv_file"`
	//  - `"csv_metric_values"`
	//  - `"datapoints_outside_range"`
	//  - `"exponential_heatmap_bucket_data"`
	//  - `"flame_graph"`
	//  - `"grafana_api"`
	//  - `"linear_heatmap_bucket_data"`
	//  - `"live"`
	//  - `"logs"`
	//  - `"manual_entry"`
	//  - `"no_data_points"`
	//  - `"node_graph"`
	//  - `"predictable_csv_wave"`
	//  - `"predictable_pulse"`
	//  - `"random_walk"`
	//  - `"random_walk_table"`
	//  - `"random_walk_with_error"`
	//  - `"raw_frame"`
	//  - `"server_error_500"`
	//  - `"simulation"`
	//  - `"slow_query"`
	//  - `"streaming_client"`
	//  - `"table_static"`
	//  - `"trace"`
	//  - `"usa"`
	//  - `"variables-query"`
	ScenarioId  *DataqueryScenarioId `json:"scenarioId,omitempty"`
	SeriesCount *int64               `json:"seriesCount,omitempty"`
	Sim         *SimulationQuery     `json:"sim,omitempty"`
	SpanCount   *int64               `json:"spanCount,omitempty"`
	Spread      *float64             `json:"spread,omitempty"`
	StartValue  *float64             `json:"startValue,omitempty"`
	Stream      *StreamingQuery      `json:"stream,omitempty"`
	// common parameter used by many query types
	StringInput *string `json:"stringInput,omitempty"`
	// TimeRange represents the query range
	// NOTE: unlike generic /ds/query, we can now send explicit time values in each query
	// NOTE: the values for timeRange are not saved in a dashboard, they are constructed on the fly
	TimeRange *TimeRange `json:"timeRange,omitempty"`
	Usa       *USAQuery  `json:"usa,omitempty"`
	WithNil   *bool      `json:"withNil,omitempty"`
}

func (resource Dataquery) ImplementsDataqueryVariant() {}

func (resource Dataquery) DataqueryType() string {
	return ""
}

func VariantConfig() variants.DataqueryConfig {
	return variants.DataqueryConfig{
		Identifier: "",
		DataqueryUnmarshaler: func(raw []byte) (variants.Dataquery, error) {
			dataquery := &Dataquery{}

			if err := json.Unmarshal(raw, dataquery); err != nil {
				return nil, err
			}

			return dataquery, nil
		},
		GoConverter: func(input any) string {
			var dataquery Dataquery
			if cast, ok := input.(*Dataquery); ok {
				dataquery = *cast
			} else {
				dataquery = input.(Dataquery)
			}
			return DataqueryConverter(dataquery)
		},
	}
}

func (resource Dataquery) Equals(otherCandidate variants.Dataquery) bool {
	if otherCandidate == nil {
		return false
	}

	other, ok := otherCandidate.(Dataquery)
	if !ok {
		return false
	}
	if resource.Alias == nil && other.Alias != nil || resource.Alias != nil && other.Alias == nil {
		return false
	}

	if resource.Alias != nil {
		if *resource.Alias != *other.Alias {
			return false
		}
	}
	if resource.Channel == nil && other.Channel != nil || resource.Channel != nil && other.Channel == nil {
		return false
	}

	if resource.Channel != nil {
		if *resource.Channel != *other.Channel {
			return false
		}
	}
	if resource.CsvContent == nil && other.CsvContent != nil || resource.CsvContent != nil && other.CsvContent == nil {
		return false
	}

	if resource.CsvContent != nil {
		if *resource.CsvContent != *other.CsvContent {
			return false
		}
	}
	if resource.CsvFileName == nil && other.CsvFileName != nil || resource.CsvFileName != nil && other.CsvFileName == nil {
		return false
	}

	if resource.CsvFileName != nil {
		if *resource.CsvFileName != *other.CsvFileName {
			return false
		}
	}

	if len(resource.CsvWave) != len(other.CsvWave) {
		return false
	}

	for i1 := range resource.CsvWave {
		if !resource.CsvWave[i1].Equals(other.CsvWave[i1]) {
			return false
		}
	}
	if resource.Datasource == nil && other.Datasource != nil || resource.Datasource != nil && other.Datasource == nil {
		return false
	}

	if resource.Datasource != nil {
		if !resource.Datasource.Equals(*other.Datasource) {
			return false
		}
	}
	if resource.DropPercent == nil && other.DropPercent != nil || resource.DropPercent != nil && other.DropPercent == nil {
		return false
	}

	if resource.DropPercent != nil {
		if *resource.DropPercent != *other.DropPercent {
			return false
		}
	}
	if resource.ErrorType == nil && other.ErrorType != nil || resource.ErrorType != nil && other.ErrorType == nil {
		return false
	}

	if resource.ErrorType != nil {
		if *resource.ErrorType != *other.ErrorType {
			return false
		}
	}
	if resource.FlamegraphDiff == nil && other.FlamegraphDiff != nil || resource.FlamegraphDiff != nil && other.FlamegraphDiff == nil {
		return false
	}

	if resource.FlamegraphDiff != nil {
		if *resource.FlamegraphDiff != *other.FlamegraphDiff {
			return false
		}
	}
	if resource.Hide == nil && other.Hide != nil || resource.Hide != nil && other.Hide == nil {
		return false
	}

	if resource.Hide != nil {
		if *resource.Hide != *other.Hide {
			return false
		}
	}
	if resource.IntervalMs == nil && other.IntervalMs != nil || resource.IntervalMs != nil && other.IntervalMs == nil {
		return false
	}

	if resource.IntervalMs != nil {
		if *resource.IntervalMs != *other.IntervalMs {
			return false
		}
	}
	if resource.Labels == nil && other.Labels != nil || resource.Labels != nil && other.Labels == nil {
		return false
	}

	if resource.Labels != nil {
		if *resource.Labels != *other.Labels {
			return false
		}
	}
	if resource.LevelColumn == nil && other.LevelColumn != nil || resource.LevelColumn != nil && other.LevelColumn == nil {
		return false
	}

	if resource.LevelColumn != nil {
		if *resource.LevelColumn != *other.LevelColumn {
			return false
		}
	}
	if resource.Lines == nil && other.Lines != nil || resource.Lines != nil && other.Lines == nil {
		return false
	}

	if resource.Lines != nil {
		if *resource.Lines != *other.Lines {
			return false
		}
	}
	if resource.Max == nil && other.Max != nil || resource.Max != nil && other.Max == nil {
		return false
	}

	if resource.Max != nil {
		if *resource.Max != *other.Max {
			return false
		}
	}
	if resource.MaxDataPoints == nil && other.MaxDataPoints != nil || resource.MaxDataPoints != nil && other.MaxDataPoints == nil {
		return false
	}

	if resource.MaxDataPoints != nil {
		if *resource.MaxDataPoints != *other.MaxDataPoints {
			return false
		}
	}
	if resource.Min == nil && other.Min != nil || resource.Min != nil && other.Min == nil {
		return false
	}

	if resource.Min != nil {
		if *resource.Min != *other.Min {
			return false
		}
	}
	if resource.Nodes == nil && other.Nodes != nil || resource.Nodes != nil && other.Nodes == nil {
		return false
	}

	if resource.Nodes != nil {
		if !resource.Nodes.Equals(*other.Nodes) {
			return false
		}
	}
	if resource.Noise == nil && other.Noise != nil || resource.Noise != nil && other.Noise == nil {
		return false
	}

	if resource.Noise != nil {
		if *resource.Noise != *other.Noise {
			return false
		}
	}

	if len(resource.Points) != len(other.Points) {
		return false
	}

	for i1 := range resource.Points {

		if len(resource.Points[i1]) != len(other.Points[i1]) {
			return false
		}

		for i2 := range resource.Points[i1] {
			// is DeepEqual good enough here?
			if !reflect.DeepEqual(resource.Points[i1][i2], other.Points[i1][i2]) {
				return false
			}
		}
	}
	if resource.PulseWave == nil && other.PulseWave != nil || resource.PulseWave != nil && other.PulseWave == nil {
		return false
	}

	if resource.PulseWave != nil {
		if !resource.PulseWave.Equals(*other.PulseWave) {
			return false
		}
	}
	if resource.QueryType == nil && other.QueryType != nil || resource.QueryType != nil && other.QueryType == nil {
		return false
	}

	if resource.QueryType != nil {
		if *resource.QueryType != *other.QueryType {
			return false
		}
	}
	if resource.RawFrameContent == nil && other.RawFrameContent != nil || resource.RawFrameContent != nil && other.RawFrameContent == nil {
		return false
	}

	if resource.RawFrameContent != nil {
		if *resource.RawFrameContent != *other.RawFrameContent {
			return false
		}
	}
	if resource.RefId == nil && other.RefId != nil || resource.RefId != nil && other.RefId == nil {
		return false
	}

	if resource.RefId != nil {
		if *resource.RefId != *other.RefId {
			return false
		}
	}
	if resource.ResultAssertions == nil && other.ResultAssertions != nil || resource.ResultAssertions != nil && other.ResultAssertions == nil {
		return false
	}

	if resource.ResultAssertions != nil {
		if !resource.ResultAssertions.Equals(*other.ResultAssertions) {
			return false
		}
	}
	if resource.ScenarioId == nil && other.ScenarioId != nil || resource.ScenarioId != nil && other.ScenarioId == nil {
		return false
	}

	if resource.ScenarioId != nil {
		if *resource.ScenarioId != *other.ScenarioId {
			return false
		}
	}
	if resource.SeriesCount == nil && other.SeriesCount != nil || resource.SeriesCount != nil && other.SeriesCount == nil {
		return false
	}

	if resource.SeriesCount != nil {
		if *resource.SeriesCount != *other.SeriesCount {
			return false
		}
	}
	if resource.Sim == nil && other.Sim != nil || resource.Sim != nil && other.Sim == nil {
		return false
	}

	if resource.Sim != nil {
		if !resource.Sim.Equals(*other.Sim) {
			return false
		}
	}
	if resource.SpanCount == nil && other.SpanCount != nil || resource.SpanCount != nil && other.SpanCount == nil {
		return false
	}

	if resource.SpanCount != nil {
		if *resource.SpanCount != *other.SpanCount {
			return false
		}
	}
	if resource.Spread == nil && other.Spread != nil || resource.Spread != nil && other.Spread == nil {
		return false
	}

	if resource.Spread != nil {
		if *resource.Spread != *other.Spread {
			return false
		}
	}
	if resource.StartValue == nil && other.StartValue != nil || resource.StartValue != nil && other.StartValue == nil {
		return false
	}

	if resource.StartValue != nil {
		if *resource.StartValue != *other.StartValue {
			return false
		}
	}
	if resource.Stream == nil && other.Stream != nil || resource.Stream != nil && other.Stream == nil {
		return false
	}

	if resource.Stream != nil {
		if !resource.Stream.Equals(*other.Stream) {
			return false
		}
	}
	if resource.StringInput == nil && other.StringInput != nil || resource.StringInput != nil && other.StringInput == nil {
		return false
	}

	if resource.StringInput != nil {
		if *resource.StringInput != *other.StringInput {
			return false
		}
	}
	if resource.TimeRange == nil && other.TimeRange != nil || resource.TimeRange != nil && other.TimeRange == nil {
		return false
	}

	if resource.TimeRange != nil {
		if !resource.TimeRange.Equals(*other.TimeRange) {
			return false
		}
	}
	if resource.Usa == nil && other.Usa != nil || resource.Usa != nil && other.Usa == nil {
		return false
	}

	if resource.Usa != nil {
		if !resource.Usa.Equals(*other.Usa) {
			return false
		}
	}
	if resource.WithNil == nil && other.WithNil != nil || resource.WithNil != nil && other.WithNil == nil {
		return false
	}

	if resource.WithNil != nil {
		if *resource.WithNil != *other.WithNil {
			return false
		}
	}

	return true
}

type NodesQueryType string

const (
	NodesQueryTypeRandom          NodesQueryType = "random"
	NodesQueryTypeRandomEdges     NodesQueryType = "random edges"
	NodesQueryTypeResponseMedium  NodesQueryType = "response_medium"
	NodesQueryTypeResponseSmall   NodesQueryType = "response_small"
	NodesQueryTypeFeatureShowcase NodesQueryType = "feature_showcase"
)

type ResultAssertionsType string

const (
	ResultAssertionsTypeNone             ResultAssertionsType = ""
	ResultAssertionsTypeTimeseriesWide   ResultAssertionsType = "timeseries-wide"
	ResultAssertionsTypeTimeseriesLong   ResultAssertionsType = "timeseries-long"
	ResultAssertionsTypeTimeseriesMany   ResultAssertionsType = "timeseries-many"
	ResultAssertionsTypeTimeseriesMulti  ResultAssertionsType = "timeseries-multi"
	ResultAssertionsTypeDirectoryListing ResultAssertionsType = "directory-listing"
	ResultAssertionsTypeTable            ResultAssertionsType = "table"
	ResultAssertionsTypeNumericWide      ResultAssertionsType = "numeric-wide"
	ResultAssertionsTypeNumericMulti     ResultAssertionsType = "numeric-multi"
	ResultAssertionsTypeNumericLong      ResultAssertionsType = "numeric-long"
	ResultAssertionsTypeLogLines         ResultAssertionsType = "log-lines"
)

type StreamingQueryType string

const (
	StreamingQueryTypeFetch  StreamingQueryType = "fetch"
	StreamingQueryTypeLogs   StreamingQueryType = "logs"
	StreamingQueryTypeSignal StreamingQueryType = "signal"
	StreamingQueryTypeTraces StreamingQueryType = "traces"
)

type DataqueryErrorType string

const (
	DataqueryErrorTypeFrontendException  DataqueryErrorType = "frontend_exception"
	DataqueryErrorTypeFrontendObservable DataqueryErrorType = "frontend_observable"
	DataqueryErrorTypeServerPanic        DataqueryErrorType = "server_panic"
)

type DataqueryScenarioId string

const (
	DataqueryScenarioIdAnnotations                  DataqueryScenarioId = "annotations"
	DataqueryScenarioIdArrow                        DataqueryScenarioId = "arrow"
	DataqueryScenarioIdCsvContent                   DataqueryScenarioId = "csv_content"
	DataqueryScenarioIdCsvFile                      DataqueryScenarioId = "csv_file"
	DataqueryScenarioIdCsvMetricValues              DataqueryScenarioId = "csv_metric_values"
	DataqueryScenarioIdDatapointsOutsideRange       DataqueryScenarioId = "datapoints_outside_range"
	DataqueryScenarioIdExponentialHeatmapBucketData DataqueryScenarioId = "exponential_heatmap_bucket_data"
	DataqueryScenarioIdFlameGraph                   DataqueryScenarioId = "flame_graph"
	DataqueryScenarioIdGrafanaApi                   DataqueryScenarioId = "grafana_api"
	DataqueryScenarioIdLinearHeatmapBucketData      DataqueryScenarioId = "linear_heatmap_bucket_data"
	DataqueryScenarioIdLive                         DataqueryScenarioId = "live"
	DataqueryScenarioIdLogs                         DataqueryScenarioId = "logs"
	DataqueryScenarioIdManualEntry                  DataqueryScenarioId = "manual_entry"
	DataqueryScenarioIdNoDataPoints                 DataqueryScenarioId = "no_data_points"
	DataqueryScenarioIdNodeGraph                    DataqueryScenarioId = "node_graph"
	DataqueryScenarioIdPredictableCsvWave           DataqueryScenarioId = "predictable_csv_wave"
	DataqueryScenarioIdPredictablePulse             DataqueryScenarioId = "predictable_pulse"
	DataqueryScenarioIdRandomWalk                   DataqueryScenarioId = "random_walk"
	DataqueryScenarioIdRandomWalkTable              DataqueryScenarioId = "random_walk_table"
	DataqueryScenarioIdRandomWalkWithError          DataqueryScenarioId = "random_walk_with_error"
	DataqueryScenarioIdRawFrame                     DataqueryScenarioId = "raw_frame"
	DataqueryScenarioIdServerError500               DataqueryScenarioId = "server_error_500"
	DataqueryScenarioIdSimulation                   DataqueryScenarioId = "simulation"
	DataqueryScenarioIdSlowQuery                    DataqueryScenarioId = "slow_query"
	DataqueryScenarioIdStreamingClient              DataqueryScenarioId = "streaming_client"
	DataqueryScenarioIdTableStatic                  DataqueryScenarioId = "table_static"
	DataqueryScenarioIdTrace                        DataqueryScenarioId = "trace"
	DataqueryScenarioIdUsa                          DataqueryScenarioId = "usa"
	DataqueryScenarioIdVariablesQuery               DataqueryScenarioId = "variables-query"
)
