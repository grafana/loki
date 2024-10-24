// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"strings"
)

func HeatmapCalculationOptionsConverter(input HeatmapCalculationOptions) string {
	calls := []string{
		`common.NewHeatmapCalculationOptionsBuilder()`,
	}
	var buffer strings.Builder
	if input.XBuckets != nil {

		buffer.WriteString(`XBuckets(`)
		arg0 := HeatmapCalculationBucketConfigConverter(*input.XBuckets)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.YBuckets != nil {

		buffer.WriteString(`YBuckets(`)
		arg0 := HeatmapCalculationBucketConfigConverter(*input.YBuckets)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
