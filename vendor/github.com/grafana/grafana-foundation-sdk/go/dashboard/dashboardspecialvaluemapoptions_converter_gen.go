// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func DashboardSpecialValueMapOptionsConverter(input DashboardSpecialValueMapOptions) string {
	calls := []string{
		`dashboard.NewDashboardSpecialValueMapOptionsBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Match(`)
		arg0 := cog.Dump(input.Match)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	{
		buffer.WriteString(`Result(`)
		arg0 := ValueMappingResultConverter(input.Result)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	return strings.Join(calls, ".\t\n")
}
