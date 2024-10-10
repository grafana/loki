// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func AnnotationPanelFilterConverter(input AnnotationPanelFilter) string {
	calls := []string{
		`dashboard.NewAnnotationPanelFilterBuilder()`,
	}
	var buffer strings.Builder
	if input.Exclude != nil && *input.Exclude != false {

		buffer.WriteString(`Exclude(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Exclude))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Ids != nil && len(input.Ids) >= 1 {

		buffer.WriteString(`Ids(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Ids {
			tmpidsarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmpidsarg1)
		}
		arg0 := "[]uint8{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
