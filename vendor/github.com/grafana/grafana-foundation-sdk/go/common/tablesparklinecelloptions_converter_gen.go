// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func TableSparklineCellOptionsConverter(input TableSparklineCellOptions) string {
	calls := []string{
		`common.NewTableSparklineCellOptionsBuilder()`,
	}
	var buffer strings.Builder
	if input.DrawStyle != nil {

		buffer.WriteString(`DrawStyle(`)
		arg0 := cog.Dump(*input.DrawStyle)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.GradientMode != nil {

		buffer.WriteString(`GradientMode(`)
		arg0 := cog.Dump(*input.GradientMode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.ThresholdsStyle != nil {

		buffer.WriteString(`ThresholdsStyle(`)
		arg0 := GraphThresholdsStyleConfigConverter(*input.ThresholdsStyle)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Transform != nil {

		buffer.WriteString(`Transform(`)
		arg0 := cog.Dump(*input.Transform)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.LineColor != nil && *input.LineColor != "" {

		buffer.WriteString(`LineColor(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.LineColor))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.LineWidth != nil {

		buffer.WriteString(`LineWidth(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.LineWidth))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.LineInterpolation != nil {

		buffer.WriteString(`LineInterpolation(`)
		arg0 := cog.Dump(*input.LineInterpolation)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.LineStyle != nil {

		buffer.WriteString(`LineStyle(`)
		arg0 := LineStyleConverter(*input.LineStyle)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FillColor != nil && *input.FillColor != "" {

		buffer.WriteString(`FillColor(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FillColor))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FillOpacity != nil {

		buffer.WriteString(`FillOpacity(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FillOpacity))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.ShowPoints != nil {

		buffer.WriteString(`ShowPoints(`)
		arg0 := cog.Dump(*input.ShowPoints)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.PointSize != nil {

		buffer.WriteString(`PointSize(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.PointSize))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.PointColor != nil && *input.PointColor != "" {

		buffer.WriteString(`PointColor(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.PointColor))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.AxisPlacement != nil {

		buffer.WriteString(`AxisPlacement(`)
		arg0 := cog.Dump(*input.AxisPlacement)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.AxisColorMode != nil {

		buffer.WriteString(`AxisColorMode(`)
		arg0 := cog.Dump(*input.AxisColorMode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.AxisLabel != nil && *input.AxisLabel != "" {

		buffer.WriteString(`AxisLabel(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.AxisLabel))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.AxisWidth != nil {

		buffer.WriteString(`AxisWidth(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.AxisWidth))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.AxisSoftMin != nil {

		buffer.WriteString(`AxisSoftMin(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.AxisSoftMin))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.AxisSoftMax != nil {

		buffer.WriteString(`AxisSoftMax(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.AxisSoftMax))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.AxisGridShow != nil {

		buffer.WriteString(`AxisGridShow(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.AxisGridShow))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.ScaleDistribution != nil {

		buffer.WriteString(`ScaleDistribution(`)
		arg0 := ScaleDistributionConfigConverter(*input.ScaleDistribution)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.AxisCenteredZero != nil {

		buffer.WriteString(`AxisCenteredZero(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.AxisCenteredZero))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.BarAlignment != nil {

		buffer.WriteString(`BarAlignment(`)
		arg0 := cog.Dump(*input.BarAlignment)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.BarWidthFactor != nil {

		buffer.WriteString(`BarWidthFactor(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.BarWidthFactor))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Stacking != nil {

		buffer.WriteString(`Stacking(`)
		arg0 := StackingConfigConverter(*input.Stacking)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.HideFrom != nil {

		buffer.WriteString(`HideFrom(`)
		arg0 := HideSeriesConfigConverter(*input.HideFrom)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.HideValue != nil {

		buffer.WriteString(`HideValue(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.HideValue))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.InsertNulls != nil {

		buffer.WriteString(`InsertNulls(`)
		arg0 := cog.Dump(*input.InsertNulls)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.SpanNulls != nil {

		buffer.WriteString(`SpanNulls(`)
		arg0 := cog.Dump(*input.SpanNulls)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FillBelowTo != nil && *input.FillBelowTo != "" {

		buffer.WriteString(`FillBelowTo(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FillBelowTo))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.PointSymbol != nil && *input.PointSymbol != "" {

		buffer.WriteString(`PointSymbol(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.PointSymbol))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.AxisBorderShow != nil {

		buffer.WriteString(`AxisBorderShow(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.AxisBorderShow))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.BarMaxWidth != nil {

		buffer.WriteString(`BarMaxWidth(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.BarMaxWidth))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
