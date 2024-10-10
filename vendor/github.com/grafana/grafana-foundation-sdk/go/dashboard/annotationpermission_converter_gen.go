// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"strings"
)

func AnnotationPermissionConverter(input AnnotationPermission) string {
	calls := []string{
		`dashboard.NewAnnotationPermissionBuilder()`,
	}
	var buffer strings.Builder
	if input.Dashboard != nil {

		buffer.WriteString(`Dashboard(`)
		arg0 := AnnotationActionsConverter(*input.Dashboard)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Organization != nil {

		buffer.WriteString(`Organization(`)
		arg0 := AnnotationActionsConverter(*input.Organization)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}

	return strings.Join(calls, ".\t\n")
}
