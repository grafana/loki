// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func HeatmapCalculationBucketConfigConverter(input HeatmapCalculationBucketConfig) string {
	calls := []string{
		`common.NewHeatmapCalculationBucketConfigBuilder()`,
	}
	var buffer strings.Builder
	if input.Mode != nil {

		buffer.WriteString(`Mode(`)
		arg0 := cog.Dump(*input.Mode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Value != nil && *input.Value != "" {

		buffer.WriteString(`Value(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Value))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Scale != nil {

		buffer.WriteString(`Scale(`)
		arg0 := ScaleDistributionConfigConverter(*input.Scale)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
