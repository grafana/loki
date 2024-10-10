// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"fmt"
	"strings"
)

func DashboardRegexMapOptionsConverter(input DashboardRegexMapOptions) string {
	calls := []string{
		`dashboard.NewDashboardRegexMapOptionsBuilder()`,
	}
	var buffer strings.Builder
	if input.Pattern != "" {

		buffer.WriteString(`Pattern(`)
		arg0 := fmt.Sprintf("%#v", input.Pattern)
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
