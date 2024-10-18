// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func FieldColorConverter(input FieldColor) string {
	calls := []string{
		`dashboard.NewFieldColorBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Mode(`)
		arg0 := cog.Dump(input.Mode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.FixedColor != nil && *input.FixedColor != "" {

		buffer.WriteString(`FixedColor(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FixedColor))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.SeriesBy != nil {

		buffer.WriteString(`SeriesBy(`)
		arg0 := cog.Dump(*input.SeriesBy)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
