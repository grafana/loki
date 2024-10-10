// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func PointsConfigConverter(input PointsConfig) string {
	calls := []string{
		`common.NewPointsConfigBuilder()`,
	}
	var buffer strings.Builder
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
	if input.PointSymbol != nil && *input.PointSymbol != "" {

		buffer.WriteString(`PointSymbol(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.PointSymbol))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
