// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func AnnotationActionsConverter(input AnnotationActions) string {
	calls := []string{
		`dashboard.NewAnnotationActionsBuilder()`,
	}
	var buffer strings.Builder
	if input.CanAdd != nil {

		buffer.WriteString(`CanAdd(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.CanAdd))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.CanDelete != nil {

		buffer.WriteString(`CanDelete(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.CanDelete))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.CanEdit != nil {

		buffer.WriteString(`CanEdit(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.CanEdit))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
