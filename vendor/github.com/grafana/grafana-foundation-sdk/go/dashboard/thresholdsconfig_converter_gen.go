// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func ThresholdsConfigConverter(input ThresholdsConfig) string {
	calls := []string{
		`dashboard.NewThresholdsConfigBuilder()`,
	}
	var buffer strings.Builder

	{
		buffer.WriteString(`Mode(`)
		arg0 := cog.Dump(input.Mode)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()
	}

	if input.Steps != nil && len(input.Steps) >= 1 {

		buffer.WriteString(`Steps(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Steps {
			tmpstepsarg1 := cog.Dump(arg1)
			tmparg0 = append(tmparg0, tmpstepsarg1)
		}
		arg0 := "[]dashboard.Threshold{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
