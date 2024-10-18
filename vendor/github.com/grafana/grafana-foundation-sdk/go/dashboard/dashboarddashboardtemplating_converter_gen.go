// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"strings"
)

func DashboardDashboardTemplatingConverter(input DashboardDashboardTemplating) string {
	calls := []string{
		`dashboard.NewDashboardDashboardTemplatingBuilder()`,
	}
	var buffer strings.Builder
	if input.List != nil && len(input.List) >= 1 {

		buffer.WriteString(`List(`)
		tmparg0 := []string{}
		for _, arg1 := range input.List {
			var tmplistarg1 string

			if arg1.Type == "query" {
				tmplistarg1 = QueryVariableConverter(arg1)
			}

			if arg1.Type == "adhoc" {
				tmplistarg1 = AdHocVariableConverter(arg1)
			}

			if arg1.Type == "constant" {
				tmplistarg1 = ConstantVariableConverter(arg1)
			}

			if arg1.Type == "datasource" {
				tmplistarg1 = DatasourceVariableConverter(arg1)
			}

			if arg1.Type == "interval" {
				tmplistarg1 = IntervalVariableConverter(arg1)
			}

			if arg1.Type == "textbox" {
				tmplistarg1 = TextBoxVariableConverter(arg1)
			}

			if arg1.Type == "custom" {
				tmplistarg1 = CustomVariableConverter(arg1)
			}

			tmparg0 = append(tmparg0, tmplistarg1)
		}
		arg0 := "[]cog.Builder[dashboard.VariableModel]{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
