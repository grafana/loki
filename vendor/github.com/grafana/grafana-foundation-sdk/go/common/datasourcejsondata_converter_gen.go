// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func DataSourceJsonDataConverter(input DataSourceJsonData) string {
	calls := []string{
		`common.NewDataSourceJsonDataBuilder()`,
	}
	var buffer strings.Builder
	if input.AuthType != nil && *input.AuthType != "" {

		buffer.WriteString(`AuthType(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.AuthType))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.DefaultRegion != nil && *input.DefaultRegion != "" {

		buffer.WriteString(`DefaultRegion(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.DefaultRegion))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Profile != nil && *input.Profile != "" {

		buffer.WriteString(`Profile(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Profile))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.ManageAlerts != nil {

		buffer.WriteString(`ManageAlerts(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.ManageAlerts))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.AlertmanagerUid != nil && *input.AlertmanagerUid != "" {

		buffer.WriteString(`AlertmanagerUid(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.AlertmanagerUid))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
