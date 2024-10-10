// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package prometheus

import (
	"encoding/json"

	variants "github.com/grafana/grafana-foundation-sdk/go/cog/variants"
	dashboard "github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

type QueryEditorMode string

const (
	QueryEditorModeCode    QueryEditorMode = "code"
	QueryEditorModeBuilder QueryEditorMode = "builder"
)

type PromQueryFormat string

const (
	PromQueryFormatTimeSeries PromQueryFormat = "time_series"
	PromQueryFormatTable      PromQueryFormat = "table"
	PromQueryFormatHeatmap    PromQueryFormat = "heatmap"
)

type Dataquery struct {
	// The actual expression/query that will be evaluated by Prometheus
	Expr string `json:"expr"`
	// Returns only the latest value that Prometheus has scraped for the requested time series
	Instant *bool `json:"instant,omitempty"`
	// Returns a Range vector, comprised of a set of time series containing a range of data points over time for each time series
	Range *bool `json:"range,omitempty"`
	// Execute an additional query to identify interesting raw samples relevant for the given expr
	Exemplar *bool `json:"exemplar,omitempty"`
	// Specifies which editor is being used to prepare the query. It can be "code" or "builder"
	EditorMode *QueryEditorMode `json:"editorMode,omitempty"`
	// Query format to determine how to display data points in panel. It can be "time_series", "table", "heatmap"
	Format *PromQueryFormat `json:"format,omitempty"`
	// Series name override or template. Ex. {{hostname}} will be replaced with label value for hostname
	LegendFormat *string `json:"legendFormat,omitempty"`
	// @deprecated Used to specify how many times to divide max data points by. We use max data points under query options
	// See https://github.com/grafana/grafana/issues/48081
	IntervalFactor *float64 `json:"intervalFactor,omitempty"`
	// A unique identifier for the query within the list of targets.
	// In server side expressions, the refId is used as a variable name to identify results.
	// By default, the UI will assign A->Z; however setting meaningful names may be useful.
	RefId string `json:"refId"`
	// If hide is set to true, Grafana will filter out the response(s) associated with this query before returning it to the panel.
	Hide *bool `json:"hide,omitempty"`
	// Specify the query flavor
	// TODO make this required and give it a default
	QueryType *string `json:"queryType,omitempty"`
	// For mixed data sources the selected datasource is on the query level.
	// For non mixed scenarios this is undefined.
	// TODO find a better way to do this ^ that's friendly to schema
	// TODO this shouldn't be unknown but DataSourceRef | null
	Datasource *dashboard.DataSourceRef `json:"datasource,omitempty"`
	// An additional lower limit for the step parameter of the Prometheus query and for the
	// `$__interval` and `$__rate_interval` variables.
	Interval *string `json:"interval,omitempty"`
}

func (resource Dataquery) ImplementsDataqueryVariant() {}

func (resource Dataquery) DataqueryType() string {
	return "prometheus"
}

func VariantConfig() variants.DataqueryConfig {
	return variants.DataqueryConfig{
		Identifier: "prometheus",
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
	if resource.Expr != other.Expr {
		return false
	}
	if resource.Instant == nil && other.Instant != nil || resource.Instant != nil && other.Instant == nil {
		return false
	}

	if resource.Instant != nil {
		if *resource.Instant != *other.Instant {
			return false
		}
	}
	if resource.Range == nil && other.Range != nil || resource.Range != nil && other.Range == nil {
		return false
	}

	if resource.Range != nil {
		if *resource.Range != *other.Range {
			return false
		}
	}
	if resource.Exemplar == nil && other.Exemplar != nil || resource.Exemplar != nil && other.Exemplar == nil {
		return false
	}

	if resource.Exemplar != nil {
		if *resource.Exemplar != *other.Exemplar {
			return false
		}
	}
	if resource.EditorMode == nil && other.EditorMode != nil || resource.EditorMode != nil && other.EditorMode == nil {
		return false
	}

	if resource.EditorMode != nil {
		if *resource.EditorMode != *other.EditorMode {
			return false
		}
	}
	if resource.Format == nil && other.Format != nil || resource.Format != nil && other.Format == nil {
		return false
	}

	if resource.Format != nil {
		if *resource.Format != *other.Format {
			return false
		}
	}
	if resource.LegendFormat == nil && other.LegendFormat != nil || resource.LegendFormat != nil && other.LegendFormat == nil {
		return false
	}

	if resource.LegendFormat != nil {
		if *resource.LegendFormat != *other.LegendFormat {
			return false
		}
	}
	if resource.IntervalFactor == nil && other.IntervalFactor != nil || resource.IntervalFactor != nil && other.IntervalFactor == nil {
		return false
	}

	if resource.IntervalFactor != nil {
		if *resource.IntervalFactor != *other.IntervalFactor {
			return false
		}
	}
	if resource.RefId != other.RefId {
		return false
	}
	if resource.Hide == nil && other.Hide != nil || resource.Hide != nil && other.Hide == nil {
		return false
	}

	if resource.Hide != nil {
		if *resource.Hide != *other.Hide {
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
	if resource.Datasource == nil && other.Datasource != nil || resource.Datasource != nil && other.Datasource == nil {
		return false
	}

	if resource.Datasource != nil {
		if !resource.Datasource.Equals(*other.Datasource) {
			return false
		}
	}
	if resource.Interval == nil && other.Interval != nil || resource.Interval != nil && other.Interval == nil {
		return false
	}

	if resource.Interval != nil {
		if *resource.Interval != *other.Interval {
			return false
		}
	}

	return true
}
