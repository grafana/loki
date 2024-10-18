// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func SnapshotConverter(input Snapshot) string {
	calls := []string{
		`dashboard.NewSnapshotBuilder()`,
	}
	var buffer strings.Builder
	if input.Expires != "" {

		buffer.WriteString(`Expires(`)
		arg0 := fmt.Sprintf("%#v", input.Expires)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	{
		buffer.WriteString(`External(`)
		arg0 := fmt.Sprintf("%#v", input.External)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.ExternalUrl != "" {

		buffer.WriteString(`ExternalUrl(`)
		arg0 := fmt.Sprintf("%#v", input.ExternalUrl)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.OriginalUrl != "" {

		buffer.WriteString(`OriginalUrl(`)
		arg0 := fmt.Sprintf("%#v", input.OriginalUrl)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	{
		buffer.WriteString(`Id(`)
		arg0 := fmt.Sprintf("%#v", input.Id)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.Key != "" {

		buffer.WriteString(`Key(`)
		arg0 := fmt.Sprintf("%#v", input.Key)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Name != "" {

		buffer.WriteString(`Name(`)
		arg0 := fmt.Sprintf("%#v", input.Name)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	{
		buffer.WriteString(`OrgId(`)
		arg0 := fmt.Sprintf("%#v", input.OrgId)
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
	if input.Dashboard != nil {

		buffer.WriteString(`Dashboard(`)
		arg0 := DashboardConverter(*input.Dashboard)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
