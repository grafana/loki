// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func TimePickerConverter(input TimePickerConfig) string {
	calls := []string{
		`dashboard.NewTimePickerBuilder()`,
	}
	var buffer strings.Builder
	if input.Hidden != nil && *input.Hidden != false {

		buffer.WriteString(`Hidden(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Hidden))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.RefreshIntervals != nil && len(input.RefreshIntervals) >= 1 {

		buffer.WriteString(`RefreshIntervals(`)
		tmparg0 := []string{}
		for _, arg1 := range input.RefreshIntervals {
			tmprefresh_intervalsarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmprefresh_intervalsarg1)
		}
		arg0 := "[]string{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.TimeOptions != nil && len(input.TimeOptions) >= 1 {

		buffer.WriteString(`TimeOptions(`)
		tmparg0 := []string{}
		for _, arg1 := range input.TimeOptions {
			tmptime_optionsarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmptime_optionsarg1)
		}
		arg0 := "[]string{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.NowDelay != nil && *input.NowDelay != "" {

		buffer.WriteString(`NowDelay(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.NowDelay))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
