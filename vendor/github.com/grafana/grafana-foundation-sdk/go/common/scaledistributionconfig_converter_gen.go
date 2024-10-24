// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func ScaleDistributionConfigConverter(input ScaleDistributionConfig) string {
	calls := []string{
		`common.NewScaleDistributionConfigBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Type(`)
		arg0 := cog.Dump(input.Type)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.Log != nil {

		buffer.WriteString(`Log(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Log))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.LinearThreshold != nil {

		buffer.WriteString(`LinearThreshold(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.LinearThreshold))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
