// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[DashboardLink] = (*DashboardLinkBuilder)(nil)

// Links with references to other dashboards or external resources
type DashboardLinkBuilder struct {
	internal *DashboardLink
	errors   map[string]cog.BuildErrors
}

func NewDashboardLinkBuilder(title string) *DashboardLinkBuilder {
	resource := &DashboardLink{}
	builder := &DashboardLinkBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()
	builder.internal.Title = title

	return builder
}

func (builder *DashboardLinkBuilder) Build() (DashboardLink, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("DashboardLink", err)...)
	}

	if len(errs) != 0 {
		return DashboardLink{}, errs
	}

	return *builder.internal, nil
}

// Title to display with the link
func (builder *DashboardLinkBuilder) Title(title string) *DashboardLinkBuilder {
	builder.internal.Title = title

	return builder
}

// Link type. Accepted values are dashboards (to refer to another dashboard) and link (to refer to an external resource)
func (builder *DashboardLinkBuilder) Type(typeArg DashboardLinkType) *DashboardLinkBuilder {
	builder.internal.Type = typeArg

	return builder
}

// Icon name to be displayed with the link
func (builder *DashboardLinkBuilder) Icon(icon string) *DashboardLinkBuilder {
	builder.internal.Icon = icon

	return builder
}

// Tooltip to display when the user hovers their mouse over it
func (builder *DashboardLinkBuilder) Tooltip(tooltip string) *DashboardLinkBuilder {
	builder.internal.Tooltip = tooltip

	return builder
}

// Link URL. Only required/valid if the type is link
func (builder *DashboardLinkBuilder) Url(url string) *DashboardLinkBuilder {
	builder.internal.Url = &url

	return builder
}

// List of tags to limit the linked dashboards. If empty, all dashboards will be displayed. Only valid if the type is dashboards
func (builder *DashboardLinkBuilder) Tags(tags []string) *DashboardLinkBuilder {
	builder.internal.Tags = tags

	return builder
}

// If true, all dashboards links will be displayed in a dropdown. If false, all dashboards links will be displayed side by side. Only valid if the type is dashboards
func (builder *DashboardLinkBuilder) AsDropdown(asDropdown bool) *DashboardLinkBuilder {
	builder.internal.AsDropdown = asDropdown

	return builder
}

// If true, the link will be opened in a new tab
func (builder *DashboardLinkBuilder) TargetBlank(targetBlank bool) *DashboardLinkBuilder {
	builder.internal.TargetBlank = targetBlank

	return builder
}

// If true, includes current template variables values in the link as query params
func (builder *DashboardLinkBuilder) IncludeVars(includeVars bool) *DashboardLinkBuilder {
	builder.internal.IncludeVars = includeVars

	return builder
}

// If true, includes current time range in the link as query params
func (builder *DashboardLinkBuilder) KeepTime(keepTime bool) *DashboardLinkBuilder {
	builder.internal.KeepTime = keepTime

	return builder
}

func (builder *DashboardLinkBuilder) applyDefaults() {
	builder.AsDropdown(false)
	builder.TargetBlank(false)
	builder.IncludeVars(false)
	builder.KeepTime(false)
}
