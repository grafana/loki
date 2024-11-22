// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func VizLegendOptionsConverter(input VizLegendOptions) string {
	calls := []string{
		`common.NewVizLegendOptionsBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`DisplayMode(`)
		arg0 := cog.Dump(input.DisplayMode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	{
		buffer.WriteString(`Placement(`)
		arg0 := cog.Dump(input.Placement)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	{
		buffer.WriteString(`ShowLegend(`)
		arg0 := fmt.Sprintf("%#v", input.ShowLegend)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.AsTable != nil {

		buffer.WriteString(`AsTable(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.AsTable))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.IsVisible != nil {

		buffer.WriteString(`IsVisible(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.IsVisible))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.SortBy != nil && *input.SortBy != "" {

		buffer.WriteString(`SortBy(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.SortBy))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.SortDesc != nil {

		buffer.WriteString(`SortDesc(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.SortDesc))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Width != nil {

		buffer.WriteString(`Width(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Width))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Calcs != nil && len(input.Calcs) >= 1 {

		buffer.WriteString(`Calcs(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Calcs {
			tmpcalcsarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmpcalcsarg1)
		}
		arg0 := "[]string{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
