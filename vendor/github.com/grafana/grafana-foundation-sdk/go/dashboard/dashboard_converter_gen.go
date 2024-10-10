// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func DashboardConverter(input Dashboard) string {
	calls := []string{
		`dashboard.NewDashboardBuilder(` + fmt.Sprintf("%#v", cog.Unptr(input.Title)) + `)`,
	}
	var buffer strings.Builder
	if input.Id != nil {

		buffer.WriteString(`Id(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Id))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Uid != nil && *input.Uid != "" {

		buffer.WriteString(`Uid(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Uid))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Title != nil && *input.Title != "" {

		buffer.WriteString(`Title(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Title))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Description != nil && *input.Description != "" {

		buffer.WriteString(`Description(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Description))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Revision != nil {

		buffer.WriteString(`Revision(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Revision))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.GnetId != nil && *input.GnetId != "" {

		buffer.WriteString(`GnetId(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.GnetId))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Tags != nil && len(input.Tags) >= 1 {

		buffer.WriteString(`Tags(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Tags {
			tmptagsarg1 := fmt.Sprintf("%#v", arg1)
			tmparg0 = append(tmparg0, tmptagsarg1)
		}
		arg0 := "[]string{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Timezone != nil && *input.Timezone != "" && *input.Timezone != "browser" {

		buffer.WriteString(`Timezone(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Timezone))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Editable != nil && *input.Editable == true {

		buffer.WriteString(`Editable(`)
		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Editable != nil && *input.Editable == false {

		buffer.WriteString(`Readonly(`)
		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.GraphTooltip != nil {

		buffer.WriteString(`Tooltip(`)
		arg0 := cog.Dump(*input.GraphTooltip)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Time != nil && input.Time.From != "" && input.Time.From != "now-6h" && input.Time.To != "" && input.Time.To != "now" {

		buffer.WriteString(`Time(`)
		arg0 := fmt.Sprintf("%#v", input.Time.From)
		buffer.WriteString(arg0)
		buffer.WriteString(", ")
		arg1 := fmt.Sprintf("%#v", input.Time.To)
		buffer.WriteString(arg1)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Timepicker != nil {

		buffer.WriteString(`Timepicker(`)
		arg0 := TimePickerConverter(*input.Timepicker)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FiscalYearStartMonth != nil && *input.FiscalYearStartMonth != 0 {

		buffer.WriteString(`FiscalYearStartMonth(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FiscalYearStartMonth))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.LiveNow != nil {

		buffer.WriteString(`LiveNow(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.LiveNow))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.WeekStart != nil && *input.WeekStart != "" {

		buffer.WriteString(`WeekStart(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.WeekStart))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Refresh != nil && *input.Refresh != "" {

		buffer.WriteString(`Refresh(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Refresh))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Version != nil {

		buffer.WriteString(`Version(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Version))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Templating.List != nil && len(input.Templating.List) >= 1 {

		buffer.WriteString(`Variables(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Templating.List {
			var tmplistarg1 string

			if arg1.Type == "query" {
				tmplistarg1 = QueryVariableConverter(arg1)
			}

			if arg1.Type == "adhoc" {
				tmplistarg1 = AdHocVariableConverter(arg1)
			}

			if arg1.Type == "constant" {
				tmplistarg1 = ConstantVariableConverter(arg1)
			}

			if arg1.Type == "datasource" {
				tmplistarg1 = DatasourceVariableConverter(arg1)
			}

			if arg1.Type == "interval" {
				tmplistarg1 = IntervalVariableConverter(arg1)
			}

			if arg1.Type == "textbox" {
				tmplistarg1 = TextBoxVariableConverter(arg1)
			}

			if arg1.Type == "custom" {
				tmplistarg1 = CustomVariableConverter(arg1)
			}

			tmparg0 = append(tmparg0, tmplistarg1)
		}
		arg0 := "[]cog.Builder[dashboard.VariableModel]{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Annotations.List != nil && len(input.Annotations.List) >= 1 {

		buffer.WriteString(`Annotations(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Annotations.List {
			tmplistarg1 := AnnotationQueryConverter(arg1)
			tmparg0 = append(tmparg0, tmplistarg1)
		}
		arg0 := "[]cog.Builder[dashboard.AnnotationQuery]{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Links != nil && len(input.Links) >= 1 {

		buffer.WriteString(`Links(`)
		tmparg0 := []string{}
		for _, arg1 := range input.Links {
			tmplinksarg1 := DashboardLinkConverter(arg1)
			tmparg0 = append(tmparg0, tmplinksarg1)
		}
		arg0 := "[]cog.Builder[dashboard.DashboardLink]{" + strings.Join(tmparg0, ",\n") + "}"
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Snapshot != nil {

		buffer.WriteString(`Snapshot(`)
		arg0 := SnapshotConverter(*input.Snapshot)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Preload != nil {

		buffer.WriteString(`Preload(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Preload))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Panels != nil && len(input.Panels) >= 1 {
		for _, item := range input.Panels {
			if item.Panel != nil {
				buffer.WriteString(`WithPanel(`)
				arg0 := cog.ConvertPanelToCode(item.Panel, item.Panel.Type)
				buffer.WriteString(arg0)

				buffer.WriteString(")")

				calls = append(calls, buffer.String())
				buffer.Reset()
			}
			if item.RowPanel != nil {
				buffer.WriteString(`WithRow(`)
				arg0 := RowConverter(*item.RowPanel)
				buffer.WriteString(arg0)

				buffer.WriteString(")")

				calls = append(calls, buffer.String())
				buffer.Reset()
			}
		}
	}

	return strings.Join(calls, ".\t\n")
}
