// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func StackingConfigConverter(input StackingConfig) string {
	calls := []string{
		`common.NewStackingConfigBuilder()`,
	}
	var buffer strings.Builder
	if input.Mode != nil {

		buffer.WriteString(`Mode(`)
		arg0 := cog.Dump(*input.Mode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Group != nil && *input.Group != "" {

		buffer.WriteString(`Group(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Group))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
