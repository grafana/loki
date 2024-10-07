// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[DashboardDashboardTime] = (*DashboardDashboardTimeBuilder)(nil)

type DashboardDashboardTimeBuilder struct {
	internal *DashboardDashboardTime
	errors   map[string]cog.BuildErrors
}

func NewDashboardDashboardTimeBuilder() *DashboardDashboardTimeBuilder {
	resource := &DashboardDashboardTime{}
	builder := &DashboardDashboardTimeBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *DashboardDashboardTimeBuilder) Build() (DashboardDashboardTime, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("DashboardDashboardTime", err)...)
	}

	if len(errs) != 0 {
		return DashboardDashboardTime{}, errs
	}

	return *builder.internal, nil
}

func (builder *DashboardDashboardTimeBuilder) From(from string) *DashboardDashboardTimeBuilder {
	builder.internal.From = from

	return builder
}

func (builder *DashboardDashboardTimeBuilder) To(to string) *DashboardDashboardTimeBuilder {
	builder.internal.To = to

	return builder
}

func (builder *DashboardDashboardTimeBuilder) applyDefaults() {
	builder.From("now-6h")
	builder.To("now")
}
