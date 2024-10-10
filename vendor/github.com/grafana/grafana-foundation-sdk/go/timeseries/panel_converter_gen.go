// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package timeseries

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
	common "github.com/grafana/grafana-foundation-sdk/go/common"
	dashboard "github.com/grafana/grafana-foundation-sdk/go/dashboard"
)

func PanelConverter(input dashboard.Panel) string {
	calls := []string{
		`timeseries.NewPanelBuilder()`,
	}
	var buffer strings.Builder
	if input.Id != nil {

		buffer.WriteString(`Id(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Id))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Targets != nil && len(input.Targets) >= 1 {

		buffer.WriteString(`Targets(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Targets {
			tmptargetsarg1 := cog.ConvertDataqueryToCode(arg1)
			tmparg0 = append(tmparg0, tmptargetsarg1)
		}
		arg0 := "[]cog.Builder[variants.Dataquery]{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Title != nil && *input.Title != "" {

		buffer.WriteString(`Title(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Title))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Description != nil && *input.Description != "" {

		buffer.WriteString(`Description(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Description))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Transparent != nil && *input.Transparent != false {

		buffer.WriteString(`Transparent(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Transparent))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Datasource != nil {

		buffer.WriteString(`Datasource(`)
		arg0 := cog.Dump(*input.Datasource)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.GridPos != nil {

		buffer.WriteString(`GridPos(`)
		arg0 := cog.Dump(*input.GridPos)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.GridPos != nil && input.GridPos.H != 9 {

		buffer.WriteString(`Height(`)
		arg0 := fmt.Sprintf("%#v", input.GridPos.H)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.GridPos != nil && input.GridPos.W != 12 {

		buffer.WriteString(`Span(`)
		arg0 := fmt.Sprintf("%#v", input.GridPos.W)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Links != nil && len(input.Links) >= 1 {

		buffer.WriteString(`Links(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Links {
			tmplinksarg1 := dashboard.DashboardLinkConverter(arg1)
			tmparg0 = append(tmparg0, tmplinksarg1)
		}
		arg0 := "[]cog.Builder[dashboard.DashboardLink]{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Repeat != nil && *input.Repeat != "" {

		buffer.WriteString(`Repeat(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Repeat))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.RepeatDirection != nil {

		buffer.WriteString(`RepeatDirection(`)
		arg0 := cog.Dump(*input.RepeatDirection)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.MaxPerRow != nil {

		buffer.WriteString(`MaxPerRow(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.MaxPerRow))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.MaxDataPoints != nil {

		buffer.WriteString(`MaxDataPoints(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.MaxDataPoints))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Transformations != nil && len(input.Transformations) >= 1 {

		buffer.WriteString(`Transformations(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Transformations {
			tmptransformationsarg1 := cog.Dump(arg1)
			tmparg0 = append(tmparg0, tmptransformationsarg1)
		}
		arg0 := "[]dashboard.DataTransformerConfig{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Interval != nil && *input.Interval != "" {

		buffer.WriteString(`Interval(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Interval))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.TimeFrom != nil && *input.TimeFrom != "" {

		buffer.WriteString(`TimeFrom(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.TimeFrom))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.TimeShift != nil && *input.TimeShift != "" {

		buffer.WriteString(`TimeShift(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.TimeShift))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.HideTimeOverride != nil {

		buffer.WriteString(`HideTimeOverride(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.HideTimeOverride))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.LibraryPanel != nil {

		buffer.WriteString(`LibraryPanel(`)
		arg0 := cog.Dump(*input.LibraryPanel)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.CacheTimeout != nil && *input.CacheTimeout != "" {

		buffer.WriteString(`CacheTimeout(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.CacheTimeout))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.QueryCachingTTL != nil {

		buffer.WriteString(`QueryCachingTTL(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.QueryCachingTTL))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.DisplayName != nil && *input.FieldConfig.Defaults.DisplayName != "" {

		buffer.WriteString(`DisplayName(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.DisplayName))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Unit != nil && *input.FieldConfig.Defaults.Unit != "" {

		buffer.WriteString(`Unit(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Unit))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Decimals != nil {

		buffer.WriteString(`Decimals(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Decimals))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Min != nil {

		buffer.WriteString(`Min(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Min))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Max != nil {

		buffer.WriteString(`Max(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Max))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Mappings != nil && len(input.FieldConfig.Defaults.Mappings) >= 1 {

		buffer.WriteString(`Mappings(`)
		tmparg0 := []string{}
		for _, arg1 := range input.FieldConfig.Defaults.Mappings {
			tmpmappingsarg1 := cog.Dump(arg1)
			tmparg0 = append(tmparg0, tmpmappingsarg1)
		}
		arg0 := "[]dashboard.ValueMapping{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Thresholds != nil {

		buffer.WriteString(`Thresholds(`)
		arg0 := dashboard.ThresholdsConfigConverter(*input.FieldConfig.Defaults.Thresholds)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Color != nil {

		buffer.WriteString(`ColorScheme(`)
		arg0 := dashboard.FieldColorConverter(*input.FieldConfig.Defaults.Color)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.NoValue != nil && *input.FieldConfig.Defaults.NoValue != "" {

		buffer.WriteString(`NoValue(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.NoValue))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Overrides != nil && len(input.FieldConfig.Overrides) >= 1 {

		buffer.WriteString(`Overrides(`)
		tmparg0 := []string{}
		for _, arg1 := range input.FieldConfig.Overrides {
			tmpoverridesarg1 := dashboard.DashboardFieldConfigSourceOverridesConverter(arg1)
			tmparg0 = append(tmparg0, tmpoverridesarg1)
		}
		arg0 := "[]cog.Builder[dashboard.DashboardFieldConfigSourceOverrides]{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Overrides != nil && len(input.FieldConfig.Overrides) >= 1 {
		for _, item := range input.FieldConfig.Overrides {

			buffer.WriteString(`WithOverride(`)
			arg0 := cog.Dump(item.Matcher)
			buffer.WriteString(arg0)
			buffer.WriteString(", ")
			tmparg1 := []string{}
			for _, arg1 := range item.Properties {
				tmppropertiesarg1 := cog.Dump(arg1)
				tmparg1 = append(tmparg1, tmppropertiesarg1)
			}
			arg1 := "[]dashboard.DynamicConfigValue{" + strings.Join(tmparg1, ",\n") + "}"
			buffer.WriteString(arg1)

			buffer.WriteString(")")

			calls = append(calls, buffer.String())
			buffer.Reset()

		}
	}
	if input.Options != nil && input.Options.(*Options).Timezone != nil && len(input.Options.(*Options).Timezone) >= 1 {

		buffer.WriteString(`Timezone(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Options.(*Options).Timezone {
			tmptimezonearg1 := cog.Dump(arg1)
			tmparg0 = append(tmparg0, tmptimezonearg1)
		}
		arg0 := "[]common.TimeZone{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Options != nil {

		buffer.WriteString(`Legend(`)
		arg0 := common.VizLegendOptionsConverter(input.Options.(*Options).Legend)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Options != nil {

		buffer.WriteString(`Tooltip(`)
		arg0 := common.VizTooltipOptionsConverter(input.Options.(*Options).Tooltip)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Options != nil && input.Options.(*Options).Orientation != nil {

		buffer.WriteString(`Orientation(`)
		arg0 := cog.Dump(*input.Options.(*Options).Orientation)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).DrawStyle != nil {

		buffer.WriteString(`DrawStyle(`)
		arg0 := cog.Dump(*input.FieldConfig.Defaults.Custom.(*FieldConfig).DrawStyle)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).GradientMode != nil {

		buffer.WriteString(`GradientMode(`)
		arg0 := cog.Dump(*input.FieldConfig.Defaults.Custom.(*FieldConfig).GradientMode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).ThresholdsStyle != nil {

		buffer.WriteString(`ThresholdsStyle(`)
		arg0 := common.GraphThresholdsStyleConfigConverter(*input.FieldConfig.Defaults.Custom.(*FieldConfig).ThresholdsStyle)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).Transform != nil {

		buffer.WriteString(`Transform(`)
		arg0 := cog.Dump(*input.FieldConfig.Defaults.Custom.(*FieldConfig).Transform)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).LineColor != nil && *input.FieldConfig.Defaults.Custom.(*FieldConfig).LineColor != "" {

		buffer.WriteString(`LineColor(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).LineColor))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).LineWidth != nil {

		buffer.WriteString(`LineWidth(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).LineWidth))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).LineInterpolation != nil {

		buffer.WriteString(`LineInterpolation(`)
		arg0 := cog.Dump(*input.FieldConfig.Defaults.Custom.(*FieldConfig).LineInterpolation)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).LineStyle != nil {

		buffer.WriteString(`LineStyle(`)
		arg0 := common.LineStyleConverter(*input.FieldConfig.Defaults.Custom.(*FieldConfig).LineStyle)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).FillColor != nil && *input.FieldConfig.Defaults.Custom.(*FieldConfig).FillColor != "" {

		buffer.WriteString(`FillColor(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).FillColor))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).FillOpacity != nil {

		buffer.WriteString(`FillOpacity(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).FillOpacity))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).ShowPoints != nil {

		buffer.WriteString(`ShowPoints(`)
		arg0 := cog.Dump(*input.FieldConfig.Defaults.Custom.(*FieldConfig).ShowPoints)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).PointSize != nil {

		buffer.WriteString(`PointSize(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).PointSize))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).PointColor != nil && *input.FieldConfig.Defaults.Custom.(*FieldConfig).PointColor != "" {

		buffer.WriteString(`PointColor(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).PointColor))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisPlacement != nil {

		buffer.WriteString(`AxisPlacement(`)
		arg0 := cog.Dump(*input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisPlacement)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisColorMode != nil {

		buffer.WriteString(`AxisColorMode(`)
		arg0 := cog.Dump(*input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisColorMode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisLabel != nil && *input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisLabel != "" {

		buffer.WriteString(`AxisLabel(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisLabel))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisWidth != nil {

		buffer.WriteString(`AxisWidth(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisWidth))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisSoftMin != nil {

		buffer.WriteString(`AxisSoftMin(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisSoftMin))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisSoftMax != nil {

		buffer.WriteString(`AxisSoftMax(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisSoftMax))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisGridShow != nil {

		buffer.WriteString(`AxisGridShow(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisGridShow))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).ScaleDistribution != nil {

		buffer.WriteString(`ScaleDistribution(`)
		arg0 := common.ScaleDistributionConfigConverter(*input.FieldConfig.Defaults.Custom.(*FieldConfig).ScaleDistribution)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisCenteredZero != nil {

		buffer.WriteString(`AxisCenteredZero(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisCenteredZero))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).BarAlignment != nil {

		buffer.WriteString(`BarAlignment(`)
		arg0 := cog.Dump(*input.FieldConfig.Defaults.Custom.(*FieldConfig).BarAlignment)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).BarWidthFactor != nil {

		buffer.WriteString(`BarWidthFactor(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).BarWidthFactor))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).Stacking != nil {

		buffer.WriteString(`Stacking(`)
		arg0 := common.StackingConfigConverter(*input.FieldConfig.Defaults.Custom.(*FieldConfig).Stacking)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).HideFrom != nil {

		buffer.WriteString(`HideFrom(`)
		arg0 := common.HideSeriesConfigConverter(*input.FieldConfig.Defaults.Custom.(*FieldConfig).HideFrom)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).InsertNulls != nil {

		buffer.WriteString(`InsertNulls(`)
		arg0 := cog.Dump(*input.FieldConfig.Defaults.Custom.(*FieldConfig).InsertNulls)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).SpanNulls != nil {

		buffer.WriteString(`SpanNulls(`)
		arg0 := cog.Dump(*input.FieldConfig.Defaults.Custom.(*FieldConfig).SpanNulls)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).FillBelowTo != nil && *input.FieldConfig.Defaults.Custom.(*FieldConfig).FillBelowTo != "" {

		buffer.WriteString(`FillBelowTo(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).FillBelowTo))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).PointSymbol != nil && *input.FieldConfig.Defaults.Custom.(*FieldConfig).PointSymbol != "" {

		buffer.WriteString(`PointSymbol(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).PointSymbol))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisBorderShow != nil {

		buffer.WriteString(`AxisBorderShow(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).AxisBorderShow))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FieldConfig != nil && input.FieldConfig.Defaults.Custom != nil && input.FieldConfig.Defaults.Custom.(*FieldConfig).BarMaxWidth != nil {

		buffer.WriteString(`BarMaxWidth(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FieldConfig.Defaults.Custom.(*FieldConfig).BarMaxWidth))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
