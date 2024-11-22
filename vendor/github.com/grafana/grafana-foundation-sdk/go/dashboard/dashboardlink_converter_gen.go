// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func DashboardLinkConverter(input DashboardLink) string {
	calls := []string{
		`dashboard.NewDashboardLinkBuilder(` + fmt.Sprintf("%#v", input.Title) + `)`,
	}
	var buffer strings.Builder
	if input.Title != "" {

		buffer.WriteString(`Title(`)
		arg0 := fmt.Sprintf("%#v", input.Title)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	{
		buffer.WriteString(`Type(`)
		arg0 := cog.Dump(input.Type)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.Icon != "" {

		buffer.WriteString(`Icon(`)
		arg0 := fmt.Sprintf("%#v", input.Icon)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Tooltip != "" {

		buffer.WriteString(`Tooltip(`)
		arg0 := fmt.Sprintf("%#v", input.Tooltip)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Url != nil && *input.Url != "" {

		buffer.WriteString(`Url(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Url))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Tags != nil && len(input.Tags) >= 1 {

		buffer.WriteString(`Tags(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Tags {
			tmptagsarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmptagsarg1)
		}
		arg0 := "[]string{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.AsDropdown != false {

		buffer.WriteString(`AsDropdown(`)
		arg0 := fmt.Sprintf("%#v", input.AsDropdown)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.TargetBlank != false {

		buffer.WriteString(`TargetBlank(`)
		arg0 := fmt.Sprintf("%#v", input.TargetBlank)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.IncludeVars != false {

		buffer.WriteString(`IncludeVars(`)
		arg0 := fmt.Sprintf("%#v", input.IncludeVars)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.KeepTime != false {

		buffer.WriteString(`KeepTime(`)
		arg0 := fmt.Sprintf("%#v", input.KeepTime)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
