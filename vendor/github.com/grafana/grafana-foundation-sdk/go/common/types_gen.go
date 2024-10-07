// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
)

// A topic is attached to DataFrame metadata in query results.
// This specifies where the data should be used.
type DataTopic string

const (
	DataTopicSeries      DataTopic = "series"
	DataTopicAnnotations DataTopic = "annotations"
	DataTopicAlertStates DataTopic = "alertStates"
)

// TODO docs
type DataSourceJsonData struct {
	AuthType        *string `json:"authType,omitempty"`
	DefaultRegion   *string `json:"defaultRegion,omitempty"`
	Profile         *string `json:"profile,omitempty"`
	ManageAlerts    *bool   `json:"manageAlerts,omitempty"`
	AlertmanagerUid *string `json:"alertmanagerUid,omitempty"`
}

func (resource DataSourceJsonData) Equals(other DataSourceJsonData) bool {
	if resource.AuthType == nil && other.AuthType != nil || resource.AuthType != nil && other.AuthType == nil {
		return false
	}

	if resource.AuthType != nil {
		if *resource.AuthType != *other.AuthType {
			return false
		}
	}
	if resource.DefaultRegion == nil && other.DefaultRegion != nil || resource.DefaultRegion != nil && other.DefaultRegion == nil {
		return false
	}

	if resource.DefaultRegion != nil {
		if *resource.DefaultRegion != *other.DefaultRegion {
			return false
		}
	}
	if resource.Profile == nil && other.Profile != nil || resource.Profile != nil && other.Profile == nil {
		return false
	}

	if resource.Profile != nil {
		if *resource.Profile != *other.Profile {
			return false
		}
	}
	if resource.ManageAlerts == nil && other.ManageAlerts != nil || resource.ManageAlerts != nil && other.ManageAlerts == nil {
		return false
	}

	if resource.ManageAlerts != nil {
		if *resource.ManageAlerts != *other.ManageAlerts {
			return false
		}
	}
	if resource.AlertmanagerUid == nil && other.AlertmanagerUid != nil || resource.AlertmanagerUid != nil && other.AlertmanagerUid == nil {
		return false
	}

	if resource.AlertmanagerUid != nil {
		if *resource.AlertmanagerUid != *other.AlertmanagerUid {
			return false
		}
	}

	return true
}

// These are the common properties available to all queries in all datasources.
// Specific implementations will *extend* this interface, adding the required
// properties for the given context.
type DataQuery struct {
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
	Datasource any `json:"datasource,omitempty"`
}

func (resource DataQuery) Equals(other DataQuery) bool {
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
	// is DeepEqual good enough here?
	if !reflect.DeepEqual(resource.Datasource, other.Datasource) {
		return false
	}

	return true
}

type BaseDimensionConfig struct {
	// fixed: T -- will be added by each element
	Field *string `json:"field,omitempty"`
}

func (resource BaseDimensionConfig) Equals(other BaseDimensionConfig) bool {
	if resource.Field == nil && other.Field != nil || resource.Field != nil && other.Field == nil {
		return false
	}

	if resource.Field != nil {
		if *resource.Field != *other.Field {
			return false
		}
	}

	return true
}

type ScaleDimensionMode string

const (
	ScaleDimensionModeLinear ScaleDimensionMode = "linear"
	ScaleDimensionModeQuad   ScaleDimensionMode = "quad"
)

type ScaleDimensionConfig struct {
	Min   float64  `json:"min"`
	Max   float64  `json:"max"`
	Fixed *float64 `json:"fixed,omitempty"`
	// fixed: T -- will be added by each element
	Field *string `json:"field,omitempty"`
	// | *"linear"
	Mode *ScaleDimensionMode `json:"mode,omitempty"`
}

func (resource ScaleDimensionConfig) Equals(other ScaleDimensionConfig) bool {
	if resource.Min != other.Min {
		return false
	}
	if resource.Max != other.Max {
		return false
	}
	if resource.Fixed == nil && other.Fixed != nil || resource.Fixed != nil && other.Fixed == nil {
		return false
	}

	if resource.Fixed != nil {
		if *resource.Fixed != *other.Fixed {
			return false
		}
	}
	if resource.Field == nil && other.Field != nil || resource.Field != nil && other.Field == nil {
		return false
	}

	if resource.Field != nil {
		if *resource.Field != *other.Field {
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

	return true
}

type ColorDimensionConfig struct {
	// color value
	Fixed *string `json:"fixed,omitempty"`
	// fixed: T -- will be added by each element
	Field *string `json:"field,omitempty"`
}

func (resource ColorDimensionConfig) Equals(other ColorDimensionConfig) bool {
	if resource.Fixed == nil && other.Fixed != nil || resource.Fixed != nil && other.Fixed == nil {
		return false
	}

	if resource.Fixed != nil {
		if *resource.Fixed != *other.Fixed {
			return false
		}
	}
	if resource.Field == nil && other.Field != nil || resource.Field != nil && other.Field == nil {
		return false
	}

	if resource.Field != nil {
		if *resource.Field != *other.Field {
			return false
		}
	}

	return true
}

type ScalarDimensionMode string

const (
	ScalarDimensionModeMod     ScalarDimensionMode = "mod"
	ScalarDimensionModeClamped ScalarDimensionMode = "clamped"
)

type ScalarDimensionConfig struct {
	Min   float64  `json:"min"`
	Max   float64  `json:"max"`
	Fixed *float64 `json:"fixed,omitempty"`
	// fixed: T -- will be added by each element
	Field *string              `json:"field,omitempty"`
	Mode  *ScalarDimensionMode `json:"mode,omitempty"`
}

func (resource ScalarDimensionConfig) Equals(other ScalarDimensionConfig) bool {
	if resource.Min != other.Min {
		return false
	}
	if resource.Max != other.Max {
		return false
	}
	if resource.Fixed == nil && other.Fixed != nil || resource.Fixed != nil && other.Fixed == nil {
		return false
	}

	if resource.Fixed != nil {
		if *resource.Fixed != *other.Fixed {
			return false
		}
	}
	if resource.Field == nil && other.Field != nil || resource.Field != nil && other.Field == nil {
		return false
	}

	if resource.Field != nil {
		if *resource.Field != *other.Field {
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

	return true
}

type TextDimensionMode string

const (
	TextDimensionModeFixed    TextDimensionMode = "fixed"
	TextDimensionModeField    TextDimensionMode = "field"
	TextDimensionModeTemplate TextDimensionMode = "template"
)

type TextDimensionConfig struct {
	Mode TextDimensionMode `json:"mode"`
	// fixed: T -- will be added by each element
	Field *string `json:"field,omitempty"`
	Fixed *string `json:"fixed,omitempty"`
}

func (resource TextDimensionConfig) Equals(other TextDimensionConfig) bool {
	if resource.Mode != other.Mode {
		return false
	}
	if resource.Field == nil && other.Field != nil || resource.Field != nil && other.Field == nil {
		return false
	}

	if resource.Field != nil {
		if *resource.Field != *other.Field {
			return false
		}
	}
	if resource.Fixed == nil && other.Fixed != nil || resource.Fixed != nil && other.Fixed == nil {
		return false
	}

	if resource.Fixed != nil {
		if *resource.Fixed != *other.Fixed {
			return false
		}
	}

	return true
}

type ResourceDimensionMode string

const (
	ResourceDimensionModeFixed   ResourceDimensionMode = "fixed"
	ResourceDimensionModeField   ResourceDimensionMode = "field"
	ResourceDimensionModeMapping ResourceDimensionMode = "mapping"
)

type MapLayerOptions struct {
	Type string `json:"type"`
	// configured unique display name
	Name string `json:"name"`
	// Custom options depending on the type
	Config any `json:"config,omitempty"`
	// Common method to define geometry fields
	Location *FrameGeometrySource `json:"location,omitempty"`
	// Defines a frame MatcherConfig that may filter data for the given layer
	FilterData any `json:"filterData,omitempty"`
	// Common properties:
	// https://openlayers.org/en/latest/apidoc/module-ol_layer_Base-BaseLayer.html
	// Layer opacity (0-1)
	Opacity *int64 `json:"opacity,omitempty"`
	// Check tooltip (defaults to true)
	Tooltip *bool `json:"tooltip,omitempty"`
}

func (resource MapLayerOptions) Equals(other MapLayerOptions) bool {
	if resource.Type != other.Type {
		return false
	}
	if resource.Name != other.Name {
		return false
	}
	// is DeepEqual good enough here?
	if !reflect.DeepEqual(resource.Config, other.Config) {
		return false
	}
	if resource.Location == nil && other.Location != nil || resource.Location != nil && other.Location == nil {
		return false
	}

	if resource.Location != nil {
		if !resource.Location.Equals(*other.Location) {
			return false
		}
	}
	// is DeepEqual good enough here?
	if !reflect.DeepEqual(resource.FilterData, other.FilterData) {
		return false
	}
	if resource.Opacity == nil && other.Opacity != nil || resource.Opacity != nil && other.Opacity == nil {
		return false
	}

	if resource.Opacity != nil {
		if *resource.Opacity != *other.Opacity {
			return false
		}
	}
	if resource.Tooltip == nil && other.Tooltip != nil || resource.Tooltip != nil && other.Tooltip == nil {
		return false
	}

	if resource.Tooltip != nil {
		if *resource.Tooltip != *other.Tooltip {
			return false
		}
	}

	return true
}

type FrameGeometrySourceMode string

const (
	FrameGeometrySourceModeAuto    FrameGeometrySourceMode = "auto"
	FrameGeometrySourceModeGeohash FrameGeometrySourceMode = "geohash"
	FrameGeometrySourceModeCoords  FrameGeometrySourceMode = "coords"
	FrameGeometrySourceModeLookup  FrameGeometrySourceMode = "lookup"
)

type HeatmapCalculationMode string

const (
	HeatmapCalculationModeSize  HeatmapCalculationMode = "size"
	HeatmapCalculationModeCount HeatmapCalculationMode = "count"
)

type HeatmapCellLayout string

const (
	HeatmapCellLayoutLe      HeatmapCellLayout = "le"
	HeatmapCellLayoutGe      HeatmapCellLayout = "ge"
	HeatmapCellLayoutUnknown HeatmapCellLayout = "unknown"
	HeatmapCellLayoutAuto    HeatmapCellLayout = "auto"
)

type HeatmapCalculationBucketConfig struct {
	// Sets the bucket calculation mode
	Mode *HeatmapCalculationMode `json:"mode,omitempty"`
	// The number of buckets to use for the axis in the heatmap
	Value *string `json:"value,omitempty"`
	// Controls the scale of the buckets
	Scale *ScaleDistributionConfig `json:"scale,omitempty"`
}

func (resource HeatmapCalculationBucketConfig) Equals(other HeatmapCalculationBucketConfig) bool {
	if resource.Mode == nil && other.Mode != nil || resource.Mode != nil && other.Mode == nil {
		return false
	}

	if resource.Mode != nil {
		if *resource.Mode != *other.Mode {
			return false
		}
	}
	if resource.Value == nil && other.Value != nil || resource.Value != nil && other.Value == nil {
		return false
	}

	if resource.Value != nil {
		if *resource.Value != *other.Value {
			return false
		}
	}
	if resource.Scale == nil && other.Scale != nil || resource.Scale != nil && other.Scale == nil {
		return false
	}

	if resource.Scale != nil {
		if !resource.Scale.Equals(*other.Scale) {
			return false
		}
	}

	return true
}

type LogsSortOrder string

const (
	LogsSortOrderDescending LogsSortOrder = "Descending"
	LogsSortOrderAscending  LogsSortOrder = "Ascending"
)

// TODO docs
type AxisPlacement string

const (
	AxisPlacementAuto   AxisPlacement = "auto"
	AxisPlacementTop    AxisPlacement = "top"
	AxisPlacementRight  AxisPlacement = "right"
	AxisPlacementBottom AxisPlacement = "bottom"
	AxisPlacementLeft   AxisPlacement = "left"
	AxisPlacementHidden AxisPlacement = "hidden"
)

// TODO docs
type AxisColorMode string

const (
	AxisColorModeText   AxisColorMode = "text"
	AxisColorModeSeries AxisColorMode = "series"
)

// TODO docs
type VisibilityMode string

const (
	VisibilityModeAuto   VisibilityMode = "auto"
	VisibilityModeNever  VisibilityMode = "never"
	VisibilityModeAlways VisibilityMode = "always"
)

// TODO docs
type GraphDrawStyle string

const (
	GraphDrawStyleLine   GraphDrawStyle = "line"
	GraphDrawStyleBars   GraphDrawStyle = "bars"
	GraphDrawStylePoints GraphDrawStyle = "points"
)

// TODO docs
type GraphTransform string

const (
	GraphTransformConstant  GraphTransform = "constant"
	GraphTransformNegativeY GraphTransform = "negative-Y"
)

// TODO docs
type LineInterpolation string

const (
	LineInterpolationLinear     LineInterpolation = "linear"
	LineInterpolationSmooth     LineInterpolation = "smooth"
	LineInterpolationStepBefore LineInterpolation = "stepBefore"
	LineInterpolationStepAfter  LineInterpolation = "stepAfter"
)

// TODO docs
type ScaleDistribution string

const (
	ScaleDistributionLinear  ScaleDistribution = "linear"
	ScaleDistributionLog     ScaleDistribution = "log"
	ScaleDistributionOrdinal ScaleDistribution = "ordinal"
	ScaleDistributionSymlog  ScaleDistribution = "symlog"
)

// TODO docs
type GraphGradientMode string

const (
	GraphGradientModeNone    GraphGradientMode = "none"
	GraphGradientModeOpacity GraphGradientMode = "opacity"
	GraphGradientModeHue     GraphGradientMode = "hue"
	GraphGradientModeScheme  GraphGradientMode = "scheme"
)

// TODO docs
type StackingMode string

const (
	StackingModeNone    StackingMode = "none"
	StackingModeNormal  StackingMode = "normal"
	StackingModePercent StackingMode = "percent"
)

// TODO docs
type BarAlignment int64

const (
	BarAlignmentBefore BarAlignment = -1
	BarAlignmentCenter BarAlignment = 0
	BarAlignmentAfter  BarAlignment = 1
)

// TODO docs
type ScaleOrientation int64

const (
	ScaleOrientationHorizontal ScaleOrientation = 0
	ScaleOrientationVertical   ScaleOrientation = 1
)

// TODO docs
type ScaleDirection int64

const (
	ScaleDirectionUp    ScaleDirection = 1
	ScaleDirectionRight ScaleDirection = 1
	ScaleDirectionDown  ScaleDirection = -1
	ScaleDirectionLeft  ScaleDirection = -1
)

// TODO docs
type LineStyle struct {
	Fill *LineStyleFill `json:"fill,omitempty"`
	Dash []float64      `json:"dash,omitempty"`
}

func (resource LineStyle) Equals(other LineStyle) bool {
	if resource.Fill == nil && other.Fill != nil || resource.Fill != nil && other.Fill == nil {
		return false
	}

	if resource.Fill != nil {
		if *resource.Fill != *other.Fill {
			return false
		}
	}

	if len(resource.Dash) != len(other.Dash) {
		return false
	}

	for i1 := range resource.Dash {
		if resource.Dash[i1] != other.Dash[i1] {
			return false
		}
	}

	return true
}

// TODO docs
type LineConfig struct {
	LineColor         *string            `json:"lineColor,omitempty"`
	LineWidth         *float64           `json:"lineWidth,omitempty"`
	LineInterpolation *LineInterpolation `json:"lineInterpolation,omitempty"`
	LineStyle         *LineStyle         `json:"lineStyle,omitempty"`
	// Indicate if null values should be treated as gaps or connected.
	// When the value is a number, it represents the maximum delta in the
	// X axis that should be considered connected.  For timeseries, this is milliseconds
	SpanNulls *BoolOrFloat64 `json:"spanNulls,omitempty"`
}

func (resource LineConfig) Equals(other LineConfig) bool {
	if resource.LineColor == nil && other.LineColor != nil || resource.LineColor != nil && other.LineColor == nil {
		return false
	}

	if resource.LineColor != nil {
		if *resource.LineColor != *other.LineColor {
			return false
		}
	}
	if resource.LineWidth == nil && other.LineWidth != nil || resource.LineWidth != nil && other.LineWidth == nil {
		return false
	}

	if resource.LineWidth != nil {
		if *resource.LineWidth != *other.LineWidth {
			return false
		}
	}
	if resource.LineInterpolation == nil && other.LineInterpolation != nil || resource.LineInterpolation != nil && other.LineInterpolation == nil {
		return false
	}

	if resource.LineInterpolation != nil {
		if *resource.LineInterpolation != *other.LineInterpolation {
			return false
		}
	}
	if resource.LineStyle == nil && other.LineStyle != nil || resource.LineStyle != nil && other.LineStyle == nil {
		return false
	}

	if resource.LineStyle != nil {
		if !resource.LineStyle.Equals(*other.LineStyle) {
			return false
		}
	}
	if resource.SpanNulls == nil && other.SpanNulls != nil || resource.SpanNulls != nil && other.SpanNulls == nil {
		return false
	}

	if resource.SpanNulls != nil {
		if !resource.SpanNulls.Equals(*other.SpanNulls) {
			return false
		}
	}

	return true
}

// TODO docs
type BarConfig struct {
	BarAlignment   *BarAlignment `json:"barAlignment,omitempty"`
	BarWidthFactor *float64      `json:"barWidthFactor,omitempty"`
	BarMaxWidth    *float64      `json:"barMaxWidth,omitempty"`
}

func (resource BarConfig) Equals(other BarConfig) bool {
	if resource.BarAlignment == nil && other.BarAlignment != nil || resource.BarAlignment != nil && other.BarAlignment == nil {
		return false
	}

	if resource.BarAlignment != nil {
		if *resource.BarAlignment != *other.BarAlignment {
			return false
		}
	}
	if resource.BarWidthFactor == nil && other.BarWidthFactor != nil || resource.BarWidthFactor != nil && other.BarWidthFactor == nil {
		return false
	}

	if resource.BarWidthFactor != nil {
		if *resource.BarWidthFactor != *other.BarWidthFactor {
			return false
		}
	}
	if resource.BarMaxWidth == nil && other.BarMaxWidth != nil || resource.BarMaxWidth != nil && other.BarMaxWidth == nil {
		return false
	}

	if resource.BarMaxWidth != nil {
		if *resource.BarMaxWidth != *other.BarMaxWidth {
			return false
		}
	}

	return true
}

// TODO docs
type FillConfig struct {
	FillColor   *string  `json:"fillColor,omitempty"`
	FillOpacity *float64 `json:"fillOpacity,omitempty"`
	FillBelowTo *string  `json:"fillBelowTo,omitempty"`
}

func (resource FillConfig) Equals(other FillConfig) bool {
	if resource.FillColor == nil && other.FillColor != nil || resource.FillColor != nil && other.FillColor == nil {
		return false
	}

	if resource.FillColor != nil {
		if *resource.FillColor != *other.FillColor {
			return false
		}
	}
	if resource.FillOpacity == nil && other.FillOpacity != nil || resource.FillOpacity != nil && other.FillOpacity == nil {
		return false
	}

	if resource.FillOpacity != nil {
		if *resource.FillOpacity != *other.FillOpacity {
			return false
		}
	}
	if resource.FillBelowTo == nil && other.FillBelowTo != nil || resource.FillBelowTo != nil && other.FillBelowTo == nil {
		return false
	}

	if resource.FillBelowTo != nil {
		if *resource.FillBelowTo != *other.FillBelowTo {
			return false
		}
	}

	return true
}

// TODO docs
type PointsConfig struct {
	ShowPoints  *VisibilityMode `json:"showPoints,omitempty"`
	PointSize   *float64        `json:"pointSize,omitempty"`
	PointColor  *string         `json:"pointColor,omitempty"`
	PointSymbol *string         `json:"pointSymbol,omitempty"`
}

func (resource PointsConfig) Equals(other PointsConfig) bool {
	if resource.ShowPoints == nil && other.ShowPoints != nil || resource.ShowPoints != nil && other.ShowPoints == nil {
		return false
	}

	if resource.ShowPoints != nil {
		if *resource.ShowPoints != *other.ShowPoints {
			return false
		}
	}
	if resource.PointSize == nil && other.PointSize != nil || resource.PointSize != nil && other.PointSize == nil {
		return false
	}

	if resource.PointSize != nil {
		if *resource.PointSize != *other.PointSize {
			return false
		}
	}
	if resource.PointColor == nil && other.PointColor != nil || resource.PointColor != nil && other.PointColor == nil {
		return false
	}

	if resource.PointColor != nil {
		if *resource.PointColor != *other.PointColor {
			return false
		}
	}
	if resource.PointSymbol == nil && other.PointSymbol != nil || resource.PointSymbol != nil && other.PointSymbol == nil {
		return false
	}

	if resource.PointSymbol != nil {
		if *resource.PointSymbol != *other.PointSymbol {
			return false
		}
	}

	return true
}

// TODO docs
type ScaleDistributionConfig struct {
	Type            ScaleDistribution `json:"type"`
	Log             *float64          `json:"log,omitempty"`
	LinearThreshold *float64          `json:"linearThreshold,omitempty"`
}

func (resource ScaleDistributionConfig) Equals(other ScaleDistributionConfig) bool {
	if resource.Type != other.Type {
		return false
	}
	if resource.Log == nil && other.Log != nil || resource.Log != nil && other.Log == nil {
		return false
	}

	if resource.Log != nil {
		if *resource.Log != *other.Log {
			return false
		}
	}
	if resource.LinearThreshold == nil && other.LinearThreshold != nil || resource.LinearThreshold != nil && other.LinearThreshold == nil {
		return false
	}

	if resource.LinearThreshold != nil {
		if *resource.LinearThreshold != *other.LinearThreshold {
			return false
		}
	}

	return true
}

// TODO docs
type AxisConfig struct {
	AxisPlacement     *AxisPlacement           `json:"axisPlacement,omitempty"`
	AxisColorMode     *AxisColorMode           `json:"axisColorMode,omitempty"`
	AxisLabel         *string                  `json:"axisLabel,omitempty"`
	AxisWidth         *float64                 `json:"axisWidth,omitempty"`
	AxisSoftMin       *float64                 `json:"axisSoftMin,omitempty"`
	AxisSoftMax       *float64                 `json:"axisSoftMax,omitempty"`
	AxisGridShow      *bool                    `json:"axisGridShow,omitempty"`
	ScaleDistribution *ScaleDistributionConfig `json:"scaleDistribution,omitempty"`
	AxisCenteredZero  *bool                    `json:"axisCenteredZero,omitempty"`
	AxisBorderShow    *bool                    `json:"axisBorderShow,omitempty"`
}

func (resource AxisConfig) Equals(other AxisConfig) bool {
	if resource.AxisPlacement == nil && other.AxisPlacement != nil || resource.AxisPlacement != nil && other.AxisPlacement == nil {
		return false
	}

	if resource.AxisPlacement != nil {
		if *resource.AxisPlacement != *other.AxisPlacement {
			return false
		}
	}
	if resource.AxisColorMode == nil && other.AxisColorMode != nil || resource.AxisColorMode != nil && other.AxisColorMode == nil {
		return false
	}

	if resource.AxisColorMode != nil {
		if *resource.AxisColorMode != *other.AxisColorMode {
			return false
		}
	}
	if resource.AxisLabel == nil && other.AxisLabel != nil || resource.AxisLabel != nil && other.AxisLabel == nil {
		return false
	}

	if resource.AxisLabel != nil {
		if *resource.AxisLabel != *other.AxisLabel {
			return false
		}
	}
	if resource.AxisWidth == nil && other.AxisWidth != nil || resource.AxisWidth != nil && other.AxisWidth == nil {
		return false
	}

	if resource.AxisWidth != nil {
		if *resource.AxisWidth != *other.AxisWidth {
			return false
		}
	}
	if resource.AxisSoftMin == nil && other.AxisSoftMin != nil || resource.AxisSoftMin != nil && other.AxisSoftMin == nil {
		return false
	}

	if resource.AxisSoftMin != nil {
		if *resource.AxisSoftMin != *other.AxisSoftMin {
			return false
		}
	}
	if resource.AxisSoftMax == nil && other.AxisSoftMax != nil || resource.AxisSoftMax != nil && other.AxisSoftMax == nil {
		return false
	}

	if resource.AxisSoftMax != nil {
		if *resource.AxisSoftMax != *other.AxisSoftMax {
			return false
		}
	}
	if resource.AxisGridShow == nil && other.AxisGridShow != nil || resource.AxisGridShow != nil && other.AxisGridShow == nil {
		return false
	}

	if resource.AxisGridShow != nil {
		if *resource.AxisGridShow != *other.AxisGridShow {
			return false
		}
	}
	if resource.ScaleDistribution == nil && other.ScaleDistribution != nil || resource.ScaleDistribution != nil && other.ScaleDistribution == nil {
		return false
	}

	if resource.ScaleDistribution != nil {
		if !resource.ScaleDistribution.Equals(*other.ScaleDistribution) {
			return false
		}
	}
	if resource.AxisCenteredZero == nil && other.AxisCenteredZero != nil || resource.AxisCenteredZero != nil && other.AxisCenteredZero == nil {
		return false
	}

	if resource.AxisCenteredZero != nil {
		if *resource.AxisCenteredZero != *other.AxisCenteredZero {
			return false
		}
	}
	if resource.AxisBorderShow == nil && other.AxisBorderShow != nil || resource.AxisBorderShow != nil && other.AxisBorderShow == nil {
		return false
	}

	if resource.AxisBorderShow != nil {
		if *resource.AxisBorderShow != *other.AxisBorderShow {
			return false
		}
	}

	return true
}

// TODO docs
type HideSeriesConfig struct {
	Tooltip bool `json:"tooltip"`
	Legend  bool `json:"legend"`
	Viz     bool `json:"viz"`
}

func (resource HideSeriesConfig) Equals(other HideSeriesConfig) bool {
	if resource.Tooltip != other.Tooltip {
		return false
	}
	if resource.Legend != other.Legend {
		return false
	}
	if resource.Viz != other.Viz {
		return false
	}

	return true
}

// TODO docs
type StackingConfig struct {
	Mode  *StackingMode `json:"mode,omitempty"`
	Group *string       `json:"group,omitempty"`
}

func (resource StackingConfig) Equals(other StackingConfig) bool {
	if resource.Mode == nil && other.Mode != nil || resource.Mode != nil && other.Mode == nil {
		return false
	}

	if resource.Mode != nil {
		if *resource.Mode != *other.Mode {
			return false
		}
	}
	if resource.Group == nil && other.Group != nil || resource.Group != nil && other.Group == nil {
		return false
	}

	if resource.Group != nil {
		if *resource.Group != *other.Group {
			return false
		}
	}

	return true
}

// TODO docs
type StackableFieldConfig struct {
	Stacking *StackingConfig `json:"stacking,omitempty"`
}

func (resource StackableFieldConfig) Equals(other StackableFieldConfig) bool {
	if resource.Stacking == nil && other.Stacking != nil || resource.Stacking != nil && other.Stacking == nil {
		return false
	}

	if resource.Stacking != nil {
		if !resource.Stacking.Equals(*other.Stacking) {
			return false
		}
	}

	return true
}

// TODO docs
type HideableFieldConfig struct {
	HideFrom *HideSeriesConfig `json:"hideFrom,omitempty"`
}

func (resource HideableFieldConfig) Equals(other HideableFieldConfig) bool {
	if resource.HideFrom == nil && other.HideFrom != nil || resource.HideFrom != nil && other.HideFrom == nil {
		return false
	}

	if resource.HideFrom != nil {
		if !resource.HideFrom.Equals(*other.HideFrom) {
			return false
		}
	}

	return true
}

// TODO docs
type GraphThresholdsStyleMode string

const (
	GraphThresholdsStyleModeOff           GraphThresholdsStyleMode = "off"
	GraphThresholdsStyleModeLine          GraphThresholdsStyleMode = "line"
	GraphThresholdsStyleModeDashed        GraphThresholdsStyleMode = "dashed"
	GraphThresholdsStyleModeArea          GraphThresholdsStyleMode = "area"
	GraphThresholdsStyleModeLineAndArea   GraphThresholdsStyleMode = "line+area"
	GraphThresholdsStyleModeDashedAndArea GraphThresholdsStyleMode = "dashed+area"
	GraphThresholdsStyleModeSeries        GraphThresholdsStyleMode = "series"
)

// TODO docs
type GraphThresholdsStyleConfig struct {
	Mode GraphThresholdsStyleMode `json:"mode"`
}

func (resource GraphThresholdsStyleConfig) Equals(other GraphThresholdsStyleConfig) bool {
	if resource.Mode != other.Mode {
		return false
	}

	return true
}

// TODO docs
type LegendPlacement string

const (
	LegendPlacementBottom LegendPlacement = "bottom"
	LegendPlacementRight  LegendPlacement = "right"
)

// TODO docs
// Note: "hidden" needs to remain as an option for plugins compatibility
type LegendDisplayMode string

const (
	LegendDisplayModeList   LegendDisplayMode = "list"
	LegendDisplayModeTable  LegendDisplayMode = "table"
	LegendDisplayModeHidden LegendDisplayMode = "hidden"
)

// TODO docs
type SingleStatBaseOptions struct {
	ReduceOptions ReduceDataOptions      `json:"reduceOptions"`
	Text          *VizTextDisplayOptions `json:"text,omitempty"`
	Orientation   VizOrientation         `json:"orientation"`
}

func (resource SingleStatBaseOptions) Equals(other SingleStatBaseOptions) bool {
	if !resource.ReduceOptions.Equals(other.ReduceOptions) {
		return false
	}
	if resource.Text == nil && other.Text != nil || resource.Text != nil && other.Text == nil {
		return false
	}

	if resource.Text != nil {
		if !resource.Text.Equals(*other.Text) {
			return false
		}
	}
	if resource.Orientation != other.Orientation {
		return false
	}

	return true
}

// TODO docs
type ReduceDataOptions struct {
	// If true show each row value
	Values *bool `json:"values,omitempty"`
	// if showing all values limit
	Limit *float64 `json:"limit,omitempty"`
	// When !values, pick one value for the whole field
	Calcs []string `json:"calcs"`
	// Which fields to show.  By default this is only numeric fields
	Fields *string `json:"fields,omitempty"`
}

func (resource ReduceDataOptions) Equals(other ReduceDataOptions) bool {
	if resource.Values == nil && other.Values != nil || resource.Values != nil && other.Values == nil {
		return false
	}

	if resource.Values != nil {
		if *resource.Values != *other.Values {
			return false
		}
	}
	if resource.Limit == nil && other.Limit != nil || resource.Limit != nil && other.Limit == nil {
		return false
	}

	if resource.Limit != nil {
		if *resource.Limit != *other.Limit {
			return false
		}
	}

	if len(resource.Calcs) != len(other.Calcs) {
		return false
	}

	for i1 := range resource.Calcs {
		if resource.Calcs[i1] != other.Calcs[i1] {
			return false
		}
	}
	if resource.Fields == nil && other.Fields != nil || resource.Fields != nil && other.Fields == nil {
		return false
	}

	if resource.Fields != nil {
		if *resource.Fields != *other.Fields {
			return false
		}
	}

	return true
}

// TODO docs
type VizOrientation string

const (
	VizOrientationAuto       VizOrientation = "auto"
	VizOrientationVertical   VizOrientation = "vertical"
	VizOrientationHorizontal VizOrientation = "horizontal"
)

// TODO docs
type OptionsWithTooltip struct {
	Tooltip VizTooltipOptions `json:"tooltip"`
}

func (resource OptionsWithTooltip) Equals(other OptionsWithTooltip) bool {
	if !resource.Tooltip.Equals(other.Tooltip) {
		return false
	}

	return true
}

// TODO docs
type OptionsWithLegend struct {
	Legend VizLegendOptions `json:"legend"`
}

func (resource OptionsWithLegend) Equals(other OptionsWithLegend) bool {
	if !resource.Legend.Equals(other.Legend) {
		return false
	}

	return true
}

// TODO docs
type OptionsWithTimezones struct {
	Timezone []TimeZone `json:"timezone,omitempty"`
}

func (resource OptionsWithTimezones) Equals(other OptionsWithTimezones) bool {

	if len(resource.Timezone) != len(other.Timezone) {
		return false
	}

	for i1 := range resource.Timezone {
		if resource.Timezone[i1] != other.Timezone[i1] {
			return false
		}
	}

	return true
}

// TODO docs
type OptionsWithTextFormatting struct {
	Text *VizTextDisplayOptions `json:"text,omitempty"`
}

func (resource OptionsWithTextFormatting) Equals(other OptionsWithTextFormatting) bool {
	if resource.Text == nil && other.Text != nil || resource.Text != nil && other.Text == nil {
		return false
	}

	if resource.Text != nil {
		if !resource.Text.Equals(*other.Text) {
			return false
		}
	}

	return true
}

// TODO docs
type BigValueColorMode string

const (
	BigValueColorModeValue           BigValueColorMode = "value"
	BigValueColorModeBackground      BigValueColorMode = "background"
	BigValueColorModeBackgroundSolid BigValueColorMode = "background_solid"
	BigValueColorModeNone            BigValueColorMode = "none"
)

// TODO docs
type BigValueGraphMode string

const (
	BigValueGraphModeNone BigValueGraphMode = "none"
	BigValueGraphModeLine BigValueGraphMode = "line"
	BigValueGraphModeArea BigValueGraphMode = "area"
)

// TODO docs
type BigValueJustifyMode string

const (
	BigValueJustifyModeAuto   BigValueJustifyMode = "auto"
	BigValueJustifyModeCenter BigValueJustifyMode = "center"
)

// TODO docs
type BigValueTextMode string

const (
	BigValueTextModeAuto         BigValueTextMode = "auto"
	BigValueTextModeValue        BigValueTextMode = "value"
	BigValueTextModeValueAndName BigValueTextMode = "value_and_name"
	BigValueTextModeName         BigValueTextMode = "name"
	BigValueTextModeNone         BigValueTextMode = "none"
)

// TODO docs
type PercentChangeColorMode string

const (
	PercentChangeColorModeStandard    PercentChangeColorMode = "standard"
	PercentChangeColorModeInverted    PercentChangeColorMode = "inverted"
	PercentChangeColorModeSameAsValue PercentChangeColorMode = "same_as_value"
)

// TODO -- should not be table specific!
// TODO docs
type FieldTextAlignment string

const (
	FieldTextAlignmentAuto   FieldTextAlignment = "auto"
	FieldTextAlignmentLeft   FieldTextAlignment = "left"
	FieldTextAlignmentRight  FieldTextAlignment = "right"
	FieldTextAlignmentCenter FieldTextAlignment = "center"
)

// Controls the value alignment in the TimelineChart component
type TimelineValueAlignment string

const (
	TimelineValueAlignmentCenter TimelineValueAlignment = "center"
	TimelineValueAlignmentLeft   TimelineValueAlignment = "left"
	TimelineValueAlignmentRight  TimelineValueAlignment = "right"
)

// TODO docs
type VizTextDisplayOptions struct {
	// Explicit title text size
	TitleSize *float64 `json:"titleSize,omitempty"`
	// Explicit value text size
	ValueSize *float64 `json:"valueSize,omitempty"`
}

func (resource VizTextDisplayOptions) Equals(other VizTextDisplayOptions) bool {
	if resource.TitleSize == nil && other.TitleSize != nil || resource.TitleSize != nil && other.TitleSize == nil {
		return false
	}

	if resource.TitleSize != nil {
		if *resource.TitleSize != *other.TitleSize {
			return false
		}
	}
	if resource.ValueSize == nil && other.ValueSize != nil || resource.ValueSize != nil && other.ValueSize == nil {
		return false
	}

	if resource.ValueSize != nil {
		if *resource.ValueSize != *other.ValueSize {
			return false
		}
	}

	return true
}

// TODO docs
type TooltipDisplayMode string

const (
	TooltipDisplayModeSingle TooltipDisplayMode = "single"
	TooltipDisplayModeMulti  TooltipDisplayMode = "multi"
	TooltipDisplayModeNone   TooltipDisplayMode = "none"
)

// TODO docs
type SortOrder string

const (
	SortOrderAscending  SortOrder = "asc"
	SortOrderDescending SortOrder = "desc"
	SortOrderNone       SortOrder = "none"
)

// TODO docs
type GraphFieldConfig struct {
	DrawStyle         *GraphDrawStyle             `json:"drawStyle,omitempty"`
	GradientMode      *GraphGradientMode          `json:"gradientMode,omitempty"`
	ThresholdsStyle   *GraphThresholdsStyleConfig `json:"thresholdsStyle,omitempty"`
	Transform         *GraphTransform             `json:"transform,omitempty"`
	LineColor         *string                     `json:"lineColor,omitempty"`
	LineWidth         *float64                    `json:"lineWidth,omitempty"`
	LineInterpolation *LineInterpolation          `json:"lineInterpolation,omitempty"`
	LineStyle         *LineStyle                  `json:"lineStyle,omitempty"`
	FillColor         *string                     `json:"fillColor,omitempty"`
	FillOpacity       *float64                    `json:"fillOpacity,omitempty"`
	ShowPoints        *VisibilityMode             `json:"showPoints,omitempty"`
	PointSize         *float64                    `json:"pointSize,omitempty"`
	PointColor        *string                     `json:"pointColor,omitempty"`
	AxisPlacement     *AxisPlacement              `json:"axisPlacement,omitempty"`
	AxisColorMode     *AxisColorMode              `json:"axisColorMode,omitempty"`
	AxisLabel         *string                     `json:"axisLabel,omitempty"`
	AxisWidth         *float64                    `json:"axisWidth,omitempty"`
	AxisSoftMin       *float64                    `json:"axisSoftMin,omitempty"`
	AxisSoftMax       *float64                    `json:"axisSoftMax,omitempty"`
	AxisGridShow      *bool                       `json:"axisGridShow,omitempty"`
	ScaleDistribution *ScaleDistributionConfig    `json:"scaleDistribution,omitempty"`
	AxisCenteredZero  *bool                       `json:"axisCenteredZero,omitempty"`
	BarAlignment      *BarAlignment               `json:"barAlignment,omitempty"`
	BarWidthFactor    *float64                    `json:"barWidthFactor,omitempty"`
	Stacking          *StackingConfig             `json:"stacking,omitempty"`
	HideFrom          *HideSeriesConfig           `json:"hideFrom,omitempty"`
	InsertNulls       *BoolOrFloat64              `json:"insertNulls,omitempty"`
	// Indicate if null values should be treated as gaps or connected.
	// When the value is a number, it represents the maximum delta in the
	// X axis that should be considered connected.  For timeseries, this is milliseconds
	SpanNulls      *BoolOrFloat64 `json:"spanNulls,omitempty"`
	FillBelowTo    *string        `json:"fillBelowTo,omitempty"`
	PointSymbol    *string        `json:"pointSymbol,omitempty"`
	AxisBorderShow *bool          `json:"axisBorderShow,omitempty"`
	BarMaxWidth    *float64       `json:"barMaxWidth,omitempty"`
}

func (resource GraphFieldConfig) Equals(other GraphFieldConfig) bool {
	if resource.DrawStyle == nil && other.DrawStyle != nil || resource.DrawStyle != nil && other.DrawStyle == nil {
		return false
	}

	if resource.DrawStyle != nil {
		if *resource.DrawStyle != *other.DrawStyle {
			return false
		}
	}
	if resource.GradientMode == nil && other.GradientMode != nil || resource.GradientMode != nil && other.GradientMode == nil {
		return false
	}

	if resource.GradientMode != nil {
		if *resource.GradientMode != *other.GradientMode {
			return false
		}
	}
	if resource.ThresholdsStyle == nil && other.ThresholdsStyle != nil || resource.ThresholdsStyle != nil && other.ThresholdsStyle == nil {
		return false
	}

	if resource.ThresholdsStyle != nil {
		if !resource.ThresholdsStyle.Equals(*other.ThresholdsStyle) {
			return false
		}
	}
	if resource.Transform == nil && other.Transform != nil || resource.Transform != nil && other.Transform == nil {
		return false
	}

	if resource.Transform != nil {
		if *resource.Transform != *other.Transform {
			return false
		}
	}
	if resource.LineColor == nil && other.LineColor != nil || resource.LineColor != nil && other.LineColor == nil {
		return false
	}

	if resource.LineColor != nil {
		if *resource.LineColor != *other.LineColor {
			return false
		}
	}
	if resource.LineWidth == nil && other.LineWidth != nil || resource.LineWidth != nil && other.LineWidth == nil {
		return false
	}

	if resource.LineWidth != nil {
		if *resource.LineWidth != *other.LineWidth {
			return false
		}
	}
	if resource.LineInterpolation == nil && other.LineInterpolation != nil || resource.LineInterpolation != nil && other.LineInterpolation == nil {
		return false
	}

	if resource.LineInterpolation != nil {
		if *resource.LineInterpolation != *other.LineInterpolation {
			return false
		}
	}
	if resource.LineStyle == nil && other.LineStyle != nil || resource.LineStyle != nil && other.LineStyle == nil {
		return false
	}

	if resource.LineStyle != nil {
		if !resource.LineStyle.Equals(*other.LineStyle) {
			return false
		}
	}
	if resource.FillColor == nil && other.FillColor != nil || resource.FillColor != nil && other.FillColor == nil {
		return false
	}

	if resource.FillColor != nil {
		if *resource.FillColor != *other.FillColor {
			return false
		}
	}
	if resource.FillOpacity == nil && other.FillOpacity != nil || resource.FillOpacity != nil && other.FillOpacity == nil {
		return false
	}

	if resource.FillOpacity != nil {
		if *resource.FillOpacity != *other.FillOpacity {
			return false
		}
	}
	if resource.ShowPoints == nil && other.ShowPoints != nil || resource.ShowPoints != nil && other.ShowPoints == nil {
		return false
	}

	if resource.ShowPoints != nil {
		if *resource.ShowPoints != *other.ShowPoints {
			return false
		}
	}
	if resource.PointSize == nil && other.PointSize != nil || resource.PointSize != nil && other.PointSize == nil {
		return false
	}

	if resource.PointSize != nil {
		if *resource.PointSize != *other.PointSize {
			return false
		}
	}
	if resource.PointColor == nil && other.PointColor != nil || resource.PointColor != nil && other.PointColor == nil {
		return false
	}

	if resource.PointColor != nil {
		if *resource.PointColor != *other.PointColor {
			return false
		}
	}
	if resource.AxisPlacement == nil && other.AxisPlacement != nil || resource.AxisPlacement != nil && other.AxisPlacement == nil {
		return false
	}

	if resource.AxisPlacement != nil {
		if *resource.AxisPlacement != *other.AxisPlacement {
			return false
		}
	}
	if resource.AxisColorMode == nil && other.AxisColorMode != nil || resource.AxisColorMode != nil && other.AxisColorMode == nil {
		return false
	}

	if resource.AxisColorMode != nil {
		if *resource.AxisColorMode != *other.AxisColorMode {
			return false
		}
	}
	if resource.AxisLabel == nil && other.AxisLabel != nil || resource.AxisLabel != nil && other.AxisLabel == nil {
		return false
	}

	if resource.AxisLabel != nil {
		if *resource.AxisLabel != *other.AxisLabel {
			return false
		}
	}
	if resource.AxisWidth == nil && other.AxisWidth != nil || resource.AxisWidth != nil && other.AxisWidth == nil {
		return false
	}

	if resource.AxisWidth != nil {
		if *resource.AxisWidth != *other.AxisWidth {
			return false
		}
	}
	if resource.AxisSoftMin == nil && other.AxisSoftMin != nil || resource.AxisSoftMin != nil && other.AxisSoftMin == nil {
		return false
	}

	if resource.AxisSoftMin != nil {
		if *resource.AxisSoftMin != *other.AxisSoftMin {
			return false
		}
	}
	if resource.AxisSoftMax == nil && other.AxisSoftMax != nil || resource.AxisSoftMax != nil && other.AxisSoftMax == nil {
		return false
	}

	if resource.AxisSoftMax != nil {
		if *resource.AxisSoftMax != *other.AxisSoftMax {
			return false
		}
	}
	if resource.AxisGridShow == nil && other.AxisGridShow != nil || resource.AxisGridShow != nil && other.AxisGridShow == nil {
		return false
	}

	if resource.AxisGridShow != nil {
		if *resource.AxisGridShow != *other.AxisGridShow {
			return false
		}
	}
	if resource.ScaleDistribution == nil && other.ScaleDistribution != nil || resource.ScaleDistribution != nil && other.ScaleDistribution == nil {
		return false
	}

	if resource.ScaleDistribution != nil {
		if !resource.ScaleDistribution.Equals(*other.ScaleDistribution) {
			return false
		}
	}
	if resource.AxisCenteredZero == nil && other.AxisCenteredZero != nil || resource.AxisCenteredZero != nil && other.AxisCenteredZero == nil {
		return false
	}

	if resource.AxisCenteredZero != nil {
		if *resource.AxisCenteredZero != *other.AxisCenteredZero {
			return false
		}
	}
	if resource.BarAlignment == nil && other.BarAlignment != nil || resource.BarAlignment != nil && other.BarAlignment == nil {
		return false
	}

	if resource.BarAlignment != nil {
		if *resource.BarAlignment != *other.BarAlignment {
			return false
		}
	}
	if resource.BarWidthFactor == nil && other.BarWidthFactor != nil || resource.BarWidthFactor != nil && other.BarWidthFactor == nil {
		return false
	}

	if resource.BarWidthFactor != nil {
		if *resource.BarWidthFactor != *other.BarWidthFactor {
			return false
		}
	}
	if resource.Stacking == nil && other.Stacking != nil || resource.Stacking != nil && other.Stacking == nil {
		return false
	}

	if resource.Stacking != nil {
		if !resource.Stacking.Equals(*other.Stacking) {
			return false
		}
	}
	if resource.HideFrom == nil && other.HideFrom != nil || resource.HideFrom != nil && other.HideFrom == nil {
		return false
	}

	if resource.HideFrom != nil {
		if !resource.HideFrom.Equals(*other.HideFrom) {
			return false
		}
	}
	if resource.InsertNulls == nil && other.InsertNulls != nil || resource.InsertNulls != nil && other.InsertNulls == nil {
		return false
	}

	if resource.InsertNulls != nil {
		if !resource.InsertNulls.Equals(*other.InsertNulls) {
			return false
		}
	}
	if resource.SpanNulls == nil && other.SpanNulls != nil || resource.SpanNulls != nil && other.SpanNulls == nil {
		return false
	}

	if resource.SpanNulls != nil {
		if !resource.SpanNulls.Equals(*other.SpanNulls) {
			return false
		}
	}
	if resource.FillBelowTo == nil && other.FillBelowTo != nil || resource.FillBelowTo != nil && other.FillBelowTo == nil {
		return false
	}

	if resource.FillBelowTo != nil {
		if *resource.FillBelowTo != *other.FillBelowTo {
			return false
		}
	}
	if resource.PointSymbol == nil && other.PointSymbol != nil || resource.PointSymbol != nil && other.PointSymbol == nil {
		return false
	}

	if resource.PointSymbol != nil {
		if *resource.PointSymbol != *other.PointSymbol {
			return false
		}
	}
	if resource.AxisBorderShow == nil && other.AxisBorderShow != nil || resource.AxisBorderShow != nil && other.AxisBorderShow == nil {
		return false
	}

	if resource.AxisBorderShow != nil {
		if *resource.AxisBorderShow != *other.AxisBorderShow {
			return false
		}
	}
	if resource.BarMaxWidth == nil && other.BarMaxWidth != nil || resource.BarMaxWidth != nil && other.BarMaxWidth == nil {
		return false
	}

	if resource.BarMaxWidth != nil {
		if *resource.BarMaxWidth != *other.BarMaxWidth {
			return false
		}
	}

	return true
}

// TODO docs
type VizLegendOptions struct {
	DisplayMode LegendDisplayMode `json:"displayMode"`
	Placement   LegendPlacement   `json:"placement"`
	ShowLegend  bool              `json:"showLegend"`
	AsTable     *bool             `json:"asTable,omitempty"`
	IsVisible   *bool             `json:"isVisible,omitempty"`
	SortBy      *string           `json:"sortBy,omitempty"`
	SortDesc    *bool             `json:"sortDesc,omitempty"`
	Width       *float64          `json:"width,omitempty"`
	Calcs       []string          `json:"calcs"`
}

func (resource VizLegendOptions) Equals(other VizLegendOptions) bool {
	if resource.DisplayMode != other.DisplayMode {
		return false
	}
	if resource.Placement != other.Placement {
		return false
	}
	if resource.ShowLegend != other.ShowLegend {
		return false
	}
	if resource.AsTable == nil && other.AsTable != nil || resource.AsTable != nil && other.AsTable == nil {
		return false
	}

	if resource.AsTable != nil {
		if *resource.AsTable != *other.AsTable {
			return false
		}
	}
	if resource.IsVisible == nil && other.IsVisible != nil || resource.IsVisible != nil && other.IsVisible == nil {
		return false
	}

	if resource.IsVisible != nil {
		if *resource.IsVisible != *other.IsVisible {
			return false
		}
	}
	if resource.SortBy == nil && other.SortBy != nil || resource.SortBy != nil && other.SortBy == nil {
		return false
	}

	if resource.SortBy != nil {
		if *resource.SortBy != *other.SortBy {
			return false
		}
	}
	if resource.SortDesc == nil && other.SortDesc != nil || resource.SortDesc != nil && other.SortDesc == nil {
		return false
	}

	if resource.SortDesc != nil {
		if *resource.SortDesc != *other.SortDesc {
			return false
		}
	}
	if resource.Width == nil && other.Width != nil || resource.Width != nil && other.Width == nil {
		return false
	}

	if resource.Width != nil {
		if *resource.Width != *other.Width {
			return false
		}
	}

	if len(resource.Calcs) != len(other.Calcs) {
		return false
	}

	for i1 := range resource.Calcs {
		if resource.Calcs[i1] != other.Calcs[i1] {
			return false
		}
	}

	return true
}

// Enum expressing the possible display modes
// for the bar gauge component of Grafana UI
type BarGaugeDisplayMode string

const (
	BarGaugeDisplayModeBasic    BarGaugeDisplayMode = "basic"
	BarGaugeDisplayModeLcd      BarGaugeDisplayMode = "lcd"
	BarGaugeDisplayModeGradient BarGaugeDisplayMode = "gradient"
)

// Allows for the table cell gauge display type to set the gauge mode.
type BarGaugeValueMode string

const (
	BarGaugeValueModeColor  BarGaugeValueMode = "color"
	BarGaugeValueModeText   BarGaugeValueMode = "text"
	BarGaugeValueModeHidden BarGaugeValueMode = "hidden"
)

// Allows for the bar gauge name to be placed explicitly
type BarGaugeNamePlacement string

const (
	BarGaugeNamePlacementAuto   BarGaugeNamePlacement = "auto"
	BarGaugeNamePlacementTop    BarGaugeNamePlacement = "top"
	BarGaugeNamePlacementLeft   BarGaugeNamePlacement = "left"
	BarGaugeNamePlacementHidden BarGaugeNamePlacement = "hidden"
)

// Allows for the bar gauge size to be set explicitly
type BarGaugeSizing string

const (
	BarGaugeSizingAuto   BarGaugeSizing = "auto"
	BarGaugeSizingManual BarGaugeSizing = "manual"
)

// TODO docs
type VizTooltipOptions struct {
	Mode      TooltipDisplayMode `json:"mode"`
	Sort      SortOrder          `json:"sort"`
	MaxWidth  *float64           `json:"maxWidth,omitempty"`
	MaxHeight *float64           `json:"maxHeight,omitempty"`
}

func (resource VizTooltipOptions) Equals(other VizTooltipOptions) bool {
	if resource.Mode != other.Mode {
		return false
	}
	if resource.Sort != other.Sort {
		return false
	}
	if resource.MaxWidth == nil && other.MaxWidth != nil || resource.MaxWidth != nil && other.MaxWidth == nil {
		return false
	}

	if resource.MaxWidth != nil {
		if *resource.MaxWidth != *other.MaxWidth {
			return false
		}
	}
	if resource.MaxHeight == nil && other.MaxHeight != nil || resource.MaxHeight != nil && other.MaxHeight == nil {
		return false
	}

	if resource.MaxHeight != nil {
		if *resource.MaxHeight != *other.MaxHeight {
			return false
		}
	}

	return true
}

type Labels map[string]string

// Internally, this is the "type" of cell that's being displayed
// in the table such as colored text, JSON, gauge, etc.
// The color-background-solid, gradient-gauge, and lcd-gauge
// modes are deprecated in favor of new cell subOptions
type TableCellDisplayMode string

const (
	TableCellDisplayModeAuto                 TableCellDisplayMode = "auto"
	TableCellDisplayModeColorText            TableCellDisplayMode = "color-text"
	TableCellDisplayModeColorBackground      TableCellDisplayMode = "color-background"
	TableCellDisplayModeColorBackgroundSolid TableCellDisplayMode = "color-background-solid"
	TableCellDisplayModeGradientGauge        TableCellDisplayMode = "gradient-gauge"
	TableCellDisplayModeLcdGauge             TableCellDisplayMode = "lcd-gauge"
	TableCellDisplayModeJSONView             TableCellDisplayMode = "json-view"
	TableCellDisplayModeBasicGauge           TableCellDisplayMode = "basic"
	TableCellDisplayModeImage                TableCellDisplayMode = "image"
	TableCellDisplayModeGauge                TableCellDisplayMode = "gauge"
	TableCellDisplayModeSparkline            TableCellDisplayMode = "sparkline"
	TableCellDisplayModeDataLinks            TableCellDisplayMode = "data-links"
	TableCellDisplayModeCustom               TableCellDisplayMode = "custom"
)

// Display mode to the "Colored Background" display
// mode for table cells. Either displays a solid color (basic mode)
// or a gradient.
type TableCellBackgroundDisplayMode string

const (
	TableCellBackgroundDisplayModeBasic    TableCellBackgroundDisplayMode = "basic"
	TableCellBackgroundDisplayModeGradient TableCellBackgroundDisplayMode = "gradient"
)

// Sort by field state
type TableSortByFieldState struct {
	// Sets the display name of the field to sort by
	DisplayName string `json:"displayName"`
	// Flag used to indicate descending sort order
	Desc *bool `json:"desc,omitempty"`
}

func (resource TableSortByFieldState) Equals(other TableSortByFieldState) bool {
	if resource.DisplayName != other.DisplayName {
		return false
	}
	if resource.Desc == nil && other.Desc != nil || resource.Desc != nil && other.Desc == nil {
		return false
	}

	if resource.Desc != nil {
		if *resource.Desc != *other.Desc {
			return false
		}
	}

	return true
}

// Footer options
type TableFooterOptions struct {
	Show bool `json:"show"`
	// actually 1 value
	Reducer          []string `json:"reducer"`
	Fields           []string `json:"fields,omitempty"`
	EnablePagination *bool    `json:"enablePagination,omitempty"`
	CountRows        *bool    `json:"countRows,omitempty"`
}

func (resource TableFooterOptions) Equals(other TableFooterOptions) bool {
	if resource.Show != other.Show {
		return false
	}

	if len(resource.Reducer) != len(other.Reducer) {
		return false
	}

	for i1 := range resource.Reducer {
		if resource.Reducer[i1] != other.Reducer[i1] {
			return false
		}
	}

	if len(resource.Fields) != len(other.Fields) {
		return false
	}

	for i1 := range resource.Fields {
		if resource.Fields[i1] != other.Fields[i1] {
			return false
		}
	}
	if resource.EnablePagination == nil && other.EnablePagination != nil || resource.EnablePagination != nil && other.EnablePagination == nil {
		return false
	}

	if resource.EnablePagination != nil {
		if *resource.EnablePagination != *other.EnablePagination {
			return false
		}
	}
	if resource.CountRows == nil && other.CountRows != nil || resource.CountRows != nil && other.CountRows == nil {
		return false
	}

	if resource.CountRows != nil {
		if *resource.CountRows != *other.CountRows {
			return false
		}
	}

	return true
}

// Auto mode table cell options
type TableAutoCellOptions struct {
	Type     string `json:"type"`
	WrapText *bool  `json:"wrapText,omitempty"`
}

func (resource TableAutoCellOptions) Equals(other TableAutoCellOptions) bool {
	if resource.Type != other.Type {
		return false
	}
	if resource.WrapText == nil && other.WrapText != nil || resource.WrapText != nil && other.WrapText == nil {
		return false
	}

	if resource.WrapText != nil {
		if *resource.WrapText != *other.WrapText {
			return false
		}
	}

	return true
}

// Colored text cell options
type TableColorTextCellOptions struct {
	Type     string `json:"type"`
	WrapText *bool  `json:"wrapText,omitempty"`
}

func (resource TableColorTextCellOptions) Equals(other TableColorTextCellOptions) bool {
	if resource.Type != other.Type {
		return false
	}
	if resource.WrapText == nil && other.WrapText != nil || resource.WrapText != nil && other.WrapText == nil {
		return false
	}

	if resource.WrapText != nil {
		if *resource.WrapText != *other.WrapText {
			return false
		}
	}

	return true
}

// Json view cell options
type TableJsonViewCellOptions struct {
	Type string `json:"type"`
}

func (resource TableJsonViewCellOptions) Equals(other TableJsonViewCellOptions) bool {
	if resource.Type != other.Type {
		return false
	}

	return true
}

// Json view cell options
type TableImageCellOptions struct {
	Type  string  `json:"type"`
	Alt   *string `json:"alt,omitempty"`
	Title *string `json:"title,omitempty"`
}

func (resource TableImageCellOptions) Equals(other TableImageCellOptions) bool {
	if resource.Type != other.Type {
		return false
	}
	if resource.Alt == nil && other.Alt != nil || resource.Alt != nil && other.Alt == nil {
		return false
	}

	if resource.Alt != nil {
		if *resource.Alt != *other.Alt {
			return false
		}
	}
	if resource.Title == nil && other.Title != nil || resource.Title != nil && other.Title == nil {
		return false
	}

	if resource.Title != nil {
		if *resource.Title != *other.Title {
			return false
		}
	}

	return true
}

// Show data links in the cell
type TableDataLinksCellOptions struct {
	Type string `json:"type"`
}

func (resource TableDataLinksCellOptions) Equals(other TableDataLinksCellOptions) bool {
	if resource.Type != other.Type {
		return false
	}

	return true
}

// Gauge cell options
type TableBarGaugeCellOptions struct {
	Type             string               `json:"type"`
	Mode             *BarGaugeDisplayMode `json:"mode,omitempty"`
	ValueDisplayMode *BarGaugeValueMode   `json:"valueDisplayMode,omitempty"`
}

func (resource TableBarGaugeCellOptions) Equals(other TableBarGaugeCellOptions) bool {
	if resource.Type != other.Type {
		return false
	}
	if resource.Mode == nil && other.Mode != nil || resource.Mode != nil && other.Mode == nil {
		return false
	}

	if resource.Mode != nil {
		if *resource.Mode != *other.Mode {
			return false
		}
	}
	if resource.ValueDisplayMode == nil && other.ValueDisplayMode != nil || resource.ValueDisplayMode != nil && other.ValueDisplayMode == nil {
		return false
	}

	if resource.ValueDisplayMode != nil {
		if *resource.ValueDisplayMode != *other.ValueDisplayMode {
			return false
		}
	}

	return true
}

// Sparkline cell options
type TableSparklineCellOptions struct {
	Type              string                      `json:"type"`
	DrawStyle         *GraphDrawStyle             `json:"drawStyle,omitempty"`
	GradientMode      *GraphGradientMode          `json:"gradientMode,omitempty"`
	ThresholdsStyle   *GraphThresholdsStyleConfig `json:"thresholdsStyle,omitempty"`
	Transform         *GraphTransform             `json:"transform,omitempty"`
	LineColor         *string                     `json:"lineColor,omitempty"`
	LineWidth         *float64                    `json:"lineWidth,omitempty"`
	LineInterpolation *LineInterpolation          `json:"lineInterpolation,omitempty"`
	LineStyle         *LineStyle                  `json:"lineStyle,omitempty"`
	FillColor         *string                     `json:"fillColor,omitempty"`
	FillOpacity       *float64                    `json:"fillOpacity,omitempty"`
	ShowPoints        *VisibilityMode             `json:"showPoints,omitempty"`
	PointSize         *float64                    `json:"pointSize,omitempty"`
	PointColor        *string                     `json:"pointColor,omitempty"`
	AxisPlacement     *AxisPlacement              `json:"axisPlacement,omitempty"`
	AxisColorMode     *AxisColorMode              `json:"axisColorMode,omitempty"`
	AxisLabel         *string                     `json:"axisLabel,omitempty"`
	AxisWidth         *float64                    `json:"axisWidth,omitempty"`
	AxisSoftMin       *float64                    `json:"axisSoftMin,omitempty"`
	AxisSoftMax       *float64                    `json:"axisSoftMax,omitempty"`
	AxisGridShow      *bool                       `json:"axisGridShow,omitempty"`
	ScaleDistribution *ScaleDistributionConfig    `json:"scaleDistribution,omitempty"`
	AxisCenteredZero  *bool                       `json:"axisCenteredZero,omitempty"`
	BarAlignment      *BarAlignment               `json:"barAlignment,omitempty"`
	BarWidthFactor    *float64                    `json:"barWidthFactor,omitempty"`
	Stacking          *StackingConfig             `json:"stacking,omitempty"`
	HideFrom          *HideSeriesConfig           `json:"hideFrom,omitempty"`
	HideValue         *bool                       `json:"hideValue,omitempty"`
	InsertNulls       *BoolOrFloat64              `json:"insertNulls,omitempty"`
	// Indicate if null values should be treated as gaps or connected.
	// When the value is a number, it represents the maximum delta in the
	// X axis that should be considered connected.  For timeseries, this is milliseconds
	SpanNulls      *BoolOrFloat64 `json:"spanNulls,omitempty"`
	FillBelowTo    *string        `json:"fillBelowTo,omitempty"`
	PointSymbol    *string        `json:"pointSymbol,omitempty"`
	AxisBorderShow *bool          `json:"axisBorderShow,omitempty"`
	BarMaxWidth    *float64       `json:"barMaxWidth,omitempty"`
}

func (resource TableSparklineCellOptions) Equals(other TableSparklineCellOptions) bool {
	if resource.Type != other.Type {
		return false
	}
	if resource.DrawStyle == nil && other.DrawStyle != nil || resource.DrawStyle != nil && other.DrawStyle == nil {
		return false
	}

	if resource.DrawStyle != nil {
		if *resource.DrawStyle != *other.DrawStyle {
			return false
		}
	}
	if resource.GradientMode == nil && other.GradientMode != nil || resource.GradientMode != nil && other.GradientMode == nil {
		return false
	}

	if resource.GradientMode != nil {
		if *resource.GradientMode != *other.GradientMode {
			return false
		}
	}
	if resource.ThresholdsStyle == nil && other.ThresholdsStyle != nil || resource.ThresholdsStyle != nil && other.ThresholdsStyle == nil {
		return false
	}

	if resource.ThresholdsStyle != nil {
		if !resource.ThresholdsStyle.Equals(*other.ThresholdsStyle) {
			return false
		}
	}
	if resource.Transform == nil && other.Transform != nil || resource.Transform != nil && other.Transform == nil {
		return false
	}

	if resource.Transform != nil {
		if *resource.Transform != *other.Transform {
			return false
		}
	}
	if resource.LineColor == nil && other.LineColor != nil || resource.LineColor != nil && other.LineColor == nil {
		return false
	}

	if resource.LineColor != nil {
		if *resource.LineColor != *other.LineColor {
			return false
		}
	}
	if resource.LineWidth == nil && other.LineWidth != nil || resource.LineWidth != nil && other.LineWidth == nil {
		return false
	}

	if resource.LineWidth != nil {
		if *resource.LineWidth != *other.LineWidth {
			return false
		}
	}
	if resource.LineInterpolation == nil && other.LineInterpolation != nil || resource.LineInterpolation != nil && other.LineInterpolation == nil {
		return false
	}

	if resource.LineInterpolation != nil {
		if *resource.LineInterpolation != *other.LineInterpolation {
			return false
		}
	}
	if resource.LineStyle == nil && other.LineStyle != nil || resource.LineStyle != nil && other.LineStyle == nil {
		return false
	}

	if resource.LineStyle != nil {
		if !resource.LineStyle.Equals(*other.LineStyle) {
			return false
		}
	}
	if resource.FillColor == nil && other.FillColor != nil || resource.FillColor != nil && other.FillColor == nil {
		return false
	}

	if resource.FillColor != nil {
		if *resource.FillColor != *other.FillColor {
			return false
		}
	}
	if resource.FillOpacity == nil && other.FillOpacity != nil || resource.FillOpacity != nil && other.FillOpacity == nil {
		return false
	}

	if resource.FillOpacity != nil {
		if *resource.FillOpacity != *other.FillOpacity {
			return false
		}
	}
	if resource.ShowPoints == nil && other.ShowPoints != nil || resource.ShowPoints != nil && other.ShowPoints == nil {
		return false
	}

	if resource.ShowPoints != nil {
		if *resource.ShowPoints != *other.ShowPoints {
			return false
		}
	}
	if resource.PointSize == nil && other.PointSize != nil || resource.PointSize != nil && other.PointSize == nil {
		return false
	}

	if resource.PointSize != nil {
		if *resource.PointSize != *other.PointSize {
			return false
		}
	}
	if resource.PointColor == nil && other.PointColor != nil || resource.PointColor != nil && other.PointColor == nil {
		return false
	}

	if resource.PointColor != nil {
		if *resource.PointColor != *other.PointColor {
			return false
		}
	}
	if resource.AxisPlacement == nil && other.AxisPlacement != nil || resource.AxisPlacement != nil && other.AxisPlacement == nil {
		return false
	}

	if resource.AxisPlacement != nil {
		if *resource.AxisPlacement != *other.AxisPlacement {
			return false
		}
	}
	if resource.AxisColorMode == nil && other.AxisColorMode != nil || resource.AxisColorMode != nil && other.AxisColorMode == nil {
		return false
	}

	if resource.AxisColorMode != nil {
		if *resource.AxisColorMode != *other.AxisColorMode {
			return false
		}
	}
	if resource.AxisLabel == nil && other.AxisLabel != nil || resource.AxisLabel != nil && other.AxisLabel == nil {
		return false
	}

	if resource.AxisLabel != nil {
		if *resource.AxisLabel != *other.AxisLabel {
			return false
		}
	}
	if resource.AxisWidth == nil && other.AxisWidth != nil || resource.AxisWidth != nil && other.AxisWidth == nil {
		return false
	}

	if resource.AxisWidth != nil {
		if *resource.AxisWidth != *other.AxisWidth {
			return false
		}
	}
	if resource.AxisSoftMin == nil && other.AxisSoftMin != nil || resource.AxisSoftMin != nil && other.AxisSoftMin == nil {
		return false
	}

	if resource.AxisSoftMin != nil {
		if *resource.AxisSoftMin != *other.AxisSoftMin {
			return false
		}
	}
	if resource.AxisSoftMax == nil && other.AxisSoftMax != nil || resource.AxisSoftMax != nil && other.AxisSoftMax == nil {
		return false
	}

	if resource.AxisSoftMax != nil {
		if *resource.AxisSoftMax != *other.AxisSoftMax {
			return false
		}
	}
	if resource.AxisGridShow == nil && other.AxisGridShow != nil || resource.AxisGridShow != nil && other.AxisGridShow == nil {
		return false
	}

	if resource.AxisGridShow != nil {
		if *resource.AxisGridShow != *other.AxisGridShow {
			return false
		}
	}
	if resource.ScaleDistribution == nil && other.ScaleDistribution != nil || resource.ScaleDistribution != nil && other.ScaleDistribution == nil {
		return false
	}

	if resource.ScaleDistribution != nil {
		if !resource.ScaleDistribution.Equals(*other.ScaleDistribution) {
			return false
		}
	}
	if resource.AxisCenteredZero == nil && other.AxisCenteredZero != nil || resource.AxisCenteredZero != nil && other.AxisCenteredZero == nil {
		return false
	}

	if resource.AxisCenteredZero != nil {
		if *resource.AxisCenteredZero != *other.AxisCenteredZero {
			return false
		}
	}
	if resource.BarAlignment == nil && other.BarAlignment != nil || resource.BarAlignment != nil && other.BarAlignment == nil {
		return false
	}

	if resource.BarAlignment != nil {
		if *resource.BarAlignment != *other.BarAlignment {
			return false
		}
	}
	if resource.BarWidthFactor == nil && other.BarWidthFactor != nil || resource.BarWidthFactor != nil && other.BarWidthFactor == nil {
		return false
	}

	if resource.BarWidthFactor != nil {
		if *resource.BarWidthFactor != *other.BarWidthFactor {
			return false
		}
	}
	if resource.Stacking == nil && other.Stacking != nil || resource.Stacking != nil && other.Stacking == nil {
		return false
	}

	if resource.Stacking != nil {
		if !resource.Stacking.Equals(*other.Stacking) {
			return false
		}
	}
	if resource.HideFrom == nil && other.HideFrom != nil || resource.HideFrom != nil && other.HideFrom == nil {
		return false
	}

	if resource.HideFrom != nil {
		if !resource.HideFrom.Equals(*other.HideFrom) {
			return false
		}
	}
	if resource.HideValue == nil && other.HideValue != nil || resource.HideValue != nil && other.HideValue == nil {
		return false
	}

	if resource.HideValue != nil {
		if *resource.HideValue != *other.HideValue {
			return false
		}
	}
	if resource.InsertNulls == nil && other.InsertNulls != nil || resource.InsertNulls != nil && other.InsertNulls == nil {
		return false
	}

	if resource.InsertNulls != nil {
		if !resource.InsertNulls.Equals(*other.InsertNulls) {
			return false
		}
	}
	if resource.SpanNulls == nil && other.SpanNulls != nil || resource.SpanNulls != nil && other.SpanNulls == nil {
		return false
	}

	if resource.SpanNulls != nil {
		if !resource.SpanNulls.Equals(*other.SpanNulls) {
			return false
		}
	}
	if resource.FillBelowTo == nil && other.FillBelowTo != nil || resource.FillBelowTo != nil && other.FillBelowTo == nil {
		return false
	}

	if resource.FillBelowTo != nil {
		if *resource.FillBelowTo != *other.FillBelowTo {
			return false
		}
	}
	if resource.PointSymbol == nil && other.PointSymbol != nil || resource.PointSymbol != nil && other.PointSymbol == nil {
		return false
	}

	if resource.PointSymbol != nil {
		if *resource.PointSymbol != *other.PointSymbol {
			return false
		}
	}
	if resource.AxisBorderShow == nil && other.AxisBorderShow != nil || resource.AxisBorderShow != nil && other.AxisBorderShow == nil {
		return false
	}

	if resource.AxisBorderShow != nil {
		if *resource.AxisBorderShow != *other.AxisBorderShow {
			return false
		}
	}
	if resource.BarMaxWidth == nil && other.BarMaxWidth != nil || resource.BarMaxWidth != nil && other.BarMaxWidth == nil {
		return false
	}

	if resource.BarMaxWidth != nil {
		if *resource.BarMaxWidth != *other.BarMaxWidth {
			return false
		}
	}

	return true
}

// Colored background cell options
type TableColoredBackgroundCellOptions struct {
	Type       string                          `json:"type"`
	Mode       *TableCellBackgroundDisplayMode `json:"mode,omitempty"`
	ApplyToRow *bool                           `json:"applyToRow,omitempty"`
	WrapText   *bool                           `json:"wrapText,omitempty"`
}

func (resource TableColoredBackgroundCellOptions) Equals(other TableColoredBackgroundCellOptions) bool {
	if resource.Type != other.Type {
		return false
	}
	if resource.Mode == nil && other.Mode != nil || resource.Mode != nil && other.Mode == nil {
		return false
	}

	if resource.Mode != nil {
		if *resource.Mode != *other.Mode {
			return false
		}
	}
	if resource.ApplyToRow == nil && other.ApplyToRow != nil || resource.ApplyToRow != nil && other.ApplyToRow == nil {
		return false
	}

	if resource.ApplyToRow != nil {
		if *resource.ApplyToRow != *other.ApplyToRow {
			return false
		}
	}
	if resource.WrapText == nil && other.WrapText != nil || resource.WrapText != nil && other.WrapText == nil {
		return false
	}

	if resource.WrapText != nil {
		if *resource.WrapText != *other.WrapText {
			return false
		}
	}

	return true
}

// Height of a table cell
type TableCellHeight string

const (
	TableCellHeightSm   TableCellHeight = "sm"
	TableCellHeightMd   TableCellHeight = "md"
	TableCellHeightLg   TableCellHeight = "lg"
	TableCellHeightAuto TableCellHeight = "auto"
)

// Table cell options. Each cell has a display mode
// and other potential options for that display.
type TableCellOptions = TableAutoCellOptionsOrTableSparklineCellOptionsOrTableBarGaugeCellOptionsOrTableColoredBackgroundCellOptionsOrTableColorTextCellOptionsOrTableImageCellOptionsOrTableDataLinksCellOptionsOrTableJsonViewCellOptions

// Use UTC/GMT timezone
const TimeZoneUtc = "utc"

// Use the timezone defined by end user web browser
const TimeZoneBrowser = "browser"

// Optional formats for the template variable replace functions
// See also https://grafana.com/docs/grafana/latest/dashboards/variables/variable-syntax/#advanced-variable-format-options
type VariableFormatID string

const (
	VariableFormatIDLucene        VariableFormatID = "lucene"
	VariableFormatIDRaw           VariableFormatID = "raw"
	VariableFormatIDRegex         VariableFormatID = "regex"
	VariableFormatIDPipe          VariableFormatID = "pipe"
	VariableFormatIDDistributed   VariableFormatID = "distributed"
	VariableFormatIDCSV           VariableFormatID = "csv"
	VariableFormatIDHTML          VariableFormatID = "html"
	VariableFormatIDJSON          VariableFormatID = "json"
	VariableFormatIDPercentEncode VariableFormatID = "percentencode"
	VariableFormatIDUriEncode     VariableFormatID = "uriencode"
	VariableFormatIDSingleQuote   VariableFormatID = "singlequote"
	VariableFormatIDDoubleQuote   VariableFormatID = "doublequote"
	VariableFormatIDSQLString     VariableFormatID = "sqlstring"
	VariableFormatIDDate          VariableFormatID = "date"
	VariableFormatIDGlob          VariableFormatID = "glob"
	VariableFormatIDText          VariableFormatID = "text"
	VariableFormatIDQueryParam    VariableFormatID = "queryparam"
)

// Links to a resource (image/svg path)
type ResourceDimensionConfig struct {
	Mode ResourceDimensionMode `json:"mode"`
	// fixed: T -- will be added by each element
	Field *string `json:"field,omitempty"`
	Fixed *string `json:"fixed,omitempty"`
}

func (resource ResourceDimensionConfig) Equals(other ResourceDimensionConfig) bool {
	if resource.Mode != other.Mode {
		return false
	}
	if resource.Field == nil && other.Field != nil || resource.Field != nil && other.Field == nil {
		return false
	}

	if resource.Field != nil {
		if *resource.Field != *other.Field {
			return false
		}
	}
	if resource.Fixed == nil && other.Fixed != nil || resource.Fixed != nil && other.Fixed == nil {
		return false
	}

	if resource.Fixed != nil {
		if *resource.Fixed != *other.Fixed {
			return false
		}
	}

	return true
}

type FrameGeometrySource struct {
	Mode FrameGeometrySourceMode `json:"mode"`
	// Field mappings
	Geohash   *string `json:"geohash,omitempty"`
	Latitude  *string `json:"latitude,omitempty"`
	Longitude *string `json:"longitude,omitempty"`
	Wkt       *string `json:"wkt,omitempty"`
	Lookup    *string `json:"lookup,omitempty"`
	// Path to Gazetteer
	Gazetteer *string `json:"gazetteer,omitempty"`
}

func (resource FrameGeometrySource) Equals(other FrameGeometrySource) bool {
	if resource.Mode != other.Mode {
		return false
	}
	if resource.Geohash == nil && other.Geohash != nil || resource.Geohash != nil && other.Geohash == nil {
		return false
	}

	if resource.Geohash != nil {
		if *resource.Geohash != *other.Geohash {
			return false
		}
	}
	if resource.Latitude == nil && other.Latitude != nil || resource.Latitude != nil && other.Latitude == nil {
		return false
	}

	if resource.Latitude != nil {
		if *resource.Latitude != *other.Latitude {
			return false
		}
	}
	if resource.Longitude == nil && other.Longitude != nil || resource.Longitude != nil && other.Longitude == nil {
		return false
	}

	if resource.Longitude != nil {
		if *resource.Longitude != *other.Longitude {
			return false
		}
	}
	if resource.Wkt == nil && other.Wkt != nil || resource.Wkt != nil && other.Wkt == nil {
		return false
	}

	if resource.Wkt != nil {
		if *resource.Wkt != *other.Wkt {
			return false
		}
	}
	if resource.Lookup == nil && other.Lookup != nil || resource.Lookup != nil && other.Lookup == nil {
		return false
	}

	if resource.Lookup != nil {
		if *resource.Lookup != *other.Lookup {
			return false
		}
	}
	if resource.Gazetteer == nil && other.Gazetteer != nil || resource.Gazetteer != nil && other.Gazetteer == nil {
		return false
	}

	if resource.Gazetteer != nil {
		if *resource.Gazetteer != *other.Gazetteer {
			return false
		}
	}

	return true
}

type HeatmapCalculationOptions struct {
	// The number of buckets to use for the xAxis in the heatmap
	XBuckets *HeatmapCalculationBucketConfig `json:"xBuckets,omitempty"`
	// The number of buckets to use for the yAxis in the heatmap
	YBuckets *HeatmapCalculationBucketConfig `json:"yBuckets,omitempty"`
}

func (resource HeatmapCalculationOptions) Equals(other HeatmapCalculationOptions) bool {
	if resource.XBuckets == nil && other.XBuckets != nil || resource.XBuckets != nil && other.XBuckets == nil {
		return false
	}

	if resource.XBuckets != nil {
		if !resource.XBuckets.Equals(*other.XBuckets) {
			return false
		}
	}
	if resource.YBuckets == nil && other.YBuckets != nil || resource.YBuckets != nil && other.YBuckets == nil {
		return false
	}

	if resource.YBuckets != nil {
		if !resource.YBuckets.Equals(*other.YBuckets) {
			return false
		}
	}

	return true
}

type LogsDedupStrategy string

const (
	LogsDedupStrategyNone      LogsDedupStrategy = "none"
	LogsDedupStrategyExact     LogsDedupStrategy = "exact"
	LogsDedupStrategyNumbers   LogsDedupStrategy = "numbers"
	LogsDedupStrategySignature LogsDedupStrategy = "signature"
)

// Compare two values
type ComparisonOperation string

const (
	ComparisonOperationEQ  ComparisonOperation = "eq"
	ComparisonOperationNEQ ComparisonOperation = "neq"
	ComparisonOperationLT  ComparisonOperation = "lt"
	ComparisonOperationLTE ComparisonOperation = "lte"
	ComparisonOperationGT  ComparisonOperation = "gt"
	ComparisonOperationGTE ComparisonOperation = "gte"
)

// Field options for each field within a table (e.g 10, "The String", 64.20, etc.)
// Generally defines alignment, filtering capabilties, display options, etc.
type TableFieldOptions struct {
	Width    *float64           `json:"width,omitempty"`
	MinWidth *float64           `json:"minWidth,omitempty"`
	Align    FieldTextAlignment `json:"align"`
	// This field is deprecated in favor of using cellOptions
	DisplayMode *TableCellDisplayMode `json:"displayMode,omitempty"`
	CellOptions *TableCellOptions     `json:"cellOptions,omitempty"`
	// ?? default is missing or false ??
	Hidden     *bool `json:"hidden,omitempty"`
	Inspect    bool  `json:"inspect"`
	Filterable *bool `json:"filterable,omitempty"`
	// Hides any header for a column, useful for columns that show some static content or buttons.
	HideHeader *bool `json:"hideHeader,omitempty"`
}

func (resource TableFieldOptions) Equals(other TableFieldOptions) bool {
	if resource.Width == nil && other.Width != nil || resource.Width != nil && other.Width == nil {
		return false
	}

	if resource.Width != nil {
		if *resource.Width != *other.Width {
			return false
		}
	}
	if resource.MinWidth == nil && other.MinWidth != nil || resource.MinWidth != nil && other.MinWidth == nil {
		return false
	}

	if resource.MinWidth != nil {
		if *resource.MinWidth != *other.MinWidth {
			return false
		}
	}
	if resource.Align != other.Align {
		return false
	}
	if resource.DisplayMode == nil && other.DisplayMode != nil || resource.DisplayMode != nil && other.DisplayMode == nil {
		return false
	}

	if resource.DisplayMode != nil {
		if *resource.DisplayMode != *other.DisplayMode {
			return false
		}
	}
	if resource.CellOptions == nil && other.CellOptions != nil || resource.CellOptions != nil && other.CellOptions == nil {
		return false
	}

	if resource.CellOptions != nil {
		if !resource.CellOptions.Equals(*other.CellOptions) {
			return false
		}
	}
	if resource.Hidden == nil && other.Hidden != nil || resource.Hidden != nil && other.Hidden == nil {
		return false
	}

	if resource.Hidden != nil {
		if *resource.Hidden != *other.Hidden {
			return false
		}
	}
	if resource.Inspect != other.Inspect {
		return false
	}
	if resource.Filterable == nil && other.Filterable != nil || resource.Filterable != nil && other.Filterable == nil {
		return false
	}

	if resource.Filterable != nil {
		if *resource.Filterable != *other.Filterable {
			return false
		}
	}
	if resource.HideHeader == nil && other.HideHeader != nil || resource.HideHeader != nil && other.HideHeader == nil {
		return false
	}

	if resource.HideHeader != nil {
		if *resource.HideHeader != *other.HideHeader {
			return false
		}
	}

	return true
}

// A specific timezone from https://en.wikipedia.org/wiki/Tz_database
type TimeZone string

type LineStyleFill string

const (
	LineStyleFillSolid  LineStyleFill = "solid"
	LineStyleFillDash   LineStyleFill = "dash"
	LineStyleFillDot    LineStyleFill = "dot"
	LineStyleFillSquare LineStyleFill = "square"
)

type BoolOrFloat64 struct {
	Bool    *bool    `json:"Bool,omitempty"`
	Float64 *float64 `json:"Float64,omitempty"`
}

func (resource BoolOrFloat64) MarshalJSON() ([]byte, error) {
	if resource.Bool != nil {
		return json.Marshal(resource.Bool)
	}

	if resource.Float64 != nil {
		return json.Marshal(resource.Float64)
	}

	return nil, fmt.Errorf("no value for disjunction of scalars")
}

func (resource *BoolOrFloat64) UnmarshalJSON(raw []byte) error {
	if raw == nil {
		return nil
	}

	var errList []error

	// Bool
	var Bool bool
	if err := json.Unmarshal(raw, &Bool); err != nil {
		errList = append(errList, err)
		resource.Bool = nil
	} else {
		resource.Bool = &Bool
		return nil
	}

	// Float64
	var Float64 float64
	if err := json.Unmarshal(raw, &Float64); err != nil {
		errList = append(errList, err)
		resource.Float64 = nil
	} else {
		resource.Float64 = &Float64
		return nil
	}

	return errors.Join(errList...)
}

func (resource BoolOrFloat64) Equals(other BoolOrFloat64) bool {
	if resource.Bool == nil && other.Bool != nil || resource.Bool != nil && other.Bool == nil {
		return false
	}

	if resource.Bool != nil {
		if *resource.Bool != *other.Bool {
			return false
		}
	}
	if resource.Float64 == nil && other.Float64 != nil || resource.Float64 != nil && other.Float64 == nil {
		return false
	}

	if resource.Float64 != nil {
		if *resource.Float64 != *other.Float64 {
			return false
		}
	}

	return true
}

type TableAutoCellOptionsOrTableSparklineCellOptionsOrTableBarGaugeCellOptionsOrTableColoredBackgroundCellOptionsOrTableColorTextCellOptionsOrTableImageCellOptionsOrTableDataLinksCellOptionsOrTableJsonViewCellOptions struct {
	TableAutoCellOptions              *TableAutoCellOptions              `json:"TableAutoCellOptions,omitempty"`
	TableSparklineCellOptions         *TableSparklineCellOptions         `json:"TableSparklineCellOptions,omitempty"`
	TableBarGaugeCellOptions          *TableBarGaugeCellOptions          `json:"TableBarGaugeCellOptions,omitempty"`
	TableColoredBackgroundCellOptions *TableColoredBackgroundCellOptions `json:"TableColoredBackgroundCellOptions,omitempty"`
	TableColorTextCellOptions         *TableColorTextCellOptions         `json:"TableColorTextCellOptions,omitempty"`
	TableImageCellOptions             *TableImageCellOptions             `json:"TableImageCellOptions,omitempty"`
	TableDataLinksCellOptions         *TableDataLinksCellOptions         `json:"TableDataLinksCellOptions,omitempty"`
	TableJsonViewCellOptions          *TableJsonViewCellOptions          `json:"TableJsonViewCellOptions,omitempty"`
}

func (resource TableAutoCellOptionsOrTableSparklineCellOptionsOrTableBarGaugeCellOptionsOrTableColoredBackgroundCellOptionsOrTableColorTextCellOptionsOrTableImageCellOptionsOrTableDataLinksCellOptionsOrTableJsonViewCellOptions) MarshalJSON() ([]byte, error) {
	if resource.TableAutoCellOptions != nil {
		return json.Marshal(resource.TableAutoCellOptions)
	}
	if resource.TableSparklineCellOptions != nil {
		return json.Marshal(resource.TableSparklineCellOptions)
	}
	if resource.TableBarGaugeCellOptions != nil {
		return json.Marshal(resource.TableBarGaugeCellOptions)
	}
	if resource.TableColoredBackgroundCellOptions != nil {
		return json.Marshal(resource.TableColoredBackgroundCellOptions)
	}
	if resource.TableColorTextCellOptions != nil {
		return json.Marshal(resource.TableColorTextCellOptions)
	}
	if resource.TableImageCellOptions != nil {
		return json.Marshal(resource.TableImageCellOptions)
	}
	if resource.TableDataLinksCellOptions != nil {
		return json.Marshal(resource.TableDataLinksCellOptions)
	}
	if resource.TableJsonViewCellOptions != nil {
		return json.Marshal(resource.TableJsonViewCellOptions)
	}

	return nil, fmt.Errorf("no value for disjunction of refs")
}

func (resource *TableAutoCellOptionsOrTableSparklineCellOptionsOrTableBarGaugeCellOptionsOrTableColoredBackgroundCellOptionsOrTableColorTextCellOptionsOrTableImageCellOptionsOrTableDataLinksCellOptionsOrTableJsonViewCellOptions) UnmarshalJSON(raw []byte) error {
	if raw == nil {
		return nil
	}

	// FIXME: this is wasteful, we need to find a more efficient way to unmarshal this.
	parsedAsMap := make(map[string]any)
	if err := json.Unmarshal(raw, &parsedAsMap); err != nil {
		return err
	}

	discriminator, found := parsedAsMap["type"]
	if !found {
		return errors.New("discriminator field 'type' not found in payload")
	}

	switch discriminator {
	case "auto":
		var tableAutoCellOptions TableAutoCellOptions
		if err := json.Unmarshal(raw, &tableAutoCellOptions); err != nil {
			return err
		}

		resource.TableAutoCellOptions = &tableAutoCellOptions
		return nil
	case "color-background":
		var tableColoredBackgroundCellOptions TableColoredBackgroundCellOptions
		if err := json.Unmarshal(raw, &tableColoredBackgroundCellOptions); err != nil {
			return err
		}

		resource.TableColoredBackgroundCellOptions = &tableColoredBackgroundCellOptions
		return nil
	case "color-text":
		var tableColorTextCellOptions TableColorTextCellOptions
		if err := json.Unmarshal(raw, &tableColorTextCellOptions); err != nil {
			return err
		}

		resource.TableColorTextCellOptions = &tableColorTextCellOptions
		return nil
	case "data-links":
		var tableDataLinksCellOptions TableDataLinksCellOptions
		if err := json.Unmarshal(raw, &tableDataLinksCellOptions); err != nil {
			return err
		}

		resource.TableDataLinksCellOptions = &tableDataLinksCellOptions
		return nil
	case "gauge":
		var tableBarGaugeCellOptions TableBarGaugeCellOptions
		if err := json.Unmarshal(raw, &tableBarGaugeCellOptions); err != nil {
			return err
		}

		resource.TableBarGaugeCellOptions = &tableBarGaugeCellOptions
		return nil
	case "image":
		var tableImageCellOptions TableImageCellOptions
		if err := json.Unmarshal(raw, &tableImageCellOptions); err != nil {
			return err
		}

		resource.TableImageCellOptions = &tableImageCellOptions
		return nil
	case "json-view":
		var tableJsonViewCellOptions TableJsonViewCellOptions
		if err := json.Unmarshal(raw, &tableJsonViewCellOptions); err != nil {
			return err
		}

		resource.TableJsonViewCellOptions = &tableJsonViewCellOptions
		return nil
	case "sparkline":
		var tableSparklineCellOptions TableSparklineCellOptions
		if err := json.Unmarshal(raw, &tableSparklineCellOptions); err != nil {
			return err
		}

		resource.TableSparklineCellOptions = &tableSparklineCellOptions
		return nil
	}

	return fmt.Errorf("could not unmarshal resource with `type = %v`", discriminator)
}

func (resource TableAutoCellOptionsOrTableSparklineCellOptionsOrTableBarGaugeCellOptionsOrTableColoredBackgroundCellOptionsOrTableColorTextCellOptionsOrTableImageCellOptionsOrTableDataLinksCellOptionsOrTableJsonViewCellOptions) Equals(other TableAutoCellOptionsOrTableSparklineCellOptionsOrTableBarGaugeCellOptionsOrTableColoredBackgroundCellOptionsOrTableColorTextCellOptionsOrTableImageCellOptionsOrTableDataLinksCellOptionsOrTableJsonViewCellOptions) bool {
	if resource.TableAutoCellOptions == nil && other.TableAutoCellOptions != nil || resource.TableAutoCellOptions != nil && other.TableAutoCellOptions == nil {
		return false
	}

	if resource.TableAutoCellOptions != nil {
		if !resource.TableAutoCellOptions.Equals(*other.TableAutoCellOptions) {
			return false
		}
	}
	if resource.TableSparklineCellOptions == nil && other.TableSparklineCellOptions != nil || resource.TableSparklineCellOptions != nil && other.TableSparklineCellOptions == nil {
		return false
	}

	if resource.TableSparklineCellOptions != nil {
		if !resource.TableSparklineCellOptions.Equals(*other.TableSparklineCellOptions) {
			return false
		}
	}
	if resource.TableBarGaugeCellOptions == nil && other.TableBarGaugeCellOptions != nil || resource.TableBarGaugeCellOptions != nil && other.TableBarGaugeCellOptions == nil {
		return false
	}

	if resource.TableBarGaugeCellOptions != nil {
		if !resource.TableBarGaugeCellOptions.Equals(*other.TableBarGaugeCellOptions) {
			return false
		}
	}
	if resource.TableColoredBackgroundCellOptions == nil && other.TableColoredBackgroundCellOptions != nil || resource.TableColoredBackgroundCellOptions != nil && other.TableColoredBackgroundCellOptions == nil {
		return false
	}

	if resource.TableColoredBackgroundCellOptions != nil {
		if !resource.TableColoredBackgroundCellOptions.Equals(*other.TableColoredBackgroundCellOptions) {
			return false
		}
	}
	if resource.TableColorTextCellOptions == nil && other.TableColorTextCellOptions != nil || resource.TableColorTextCellOptions != nil && other.TableColorTextCellOptions == nil {
		return false
	}

	if resource.TableColorTextCellOptions != nil {
		if !resource.TableColorTextCellOptions.Equals(*other.TableColorTextCellOptions) {
			return false
		}
	}
	if resource.TableImageCellOptions == nil && other.TableImageCellOptions != nil || resource.TableImageCellOptions != nil && other.TableImageCellOptions == nil {
		return false
	}

	if resource.TableImageCellOptions != nil {
		if !resource.TableImageCellOptions.Equals(*other.TableImageCellOptions) {
			return false
		}
	}
	if resource.TableDataLinksCellOptions == nil && other.TableDataLinksCellOptions != nil || resource.TableDataLinksCellOptions != nil && other.TableDataLinksCellOptions == nil {
		return false
	}

	if resource.TableDataLinksCellOptions != nil {
		if !resource.TableDataLinksCellOptions.Equals(*other.TableDataLinksCellOptions) {
			return false
		}
	}
	if resource.TableJsonViewCellOptions == nil && other.TableJsonViewCellOptions != nil || resource.TableJsonViewCellOptions != nil && other.TableJsonViewCellOptions == nil {
		return false
	}

	if resource.TableJsonViewCellOptions != nil {
		if !resource.TableJsonViewCellOptions.Equals(*other.TableJsonViewCellOptions) {
			return false
		}
	}

	return true
}
