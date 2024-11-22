// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func VizTextDisplayOptionsConverter(input VizTextDisplayOptions) string {
	calls := []string{
		`common.NewVizTextDisplayOptionsBuilder()`,
	}
	var buffer strings.Builder
	if input.TitleSize != nil {

		buffer.WriteString(`TitleSize(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.TitleSize))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.ValueSize != nil {

		buffer.WriteString(`ValueSize(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.ValueSize))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
