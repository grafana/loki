// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[DashboardRangeMapOptions] = (*DashboardRangeMapOptionsBuilder)(nil)

type DashboardRangeMapOptionsBuilder struct {
	internal *DashboardRangeMapOptions
	errors   map[string]cog.BuildErrors
}

func NewDashboardRangeMapOptionsBuilder() *DashboardRangeMapOptionsBuilder {
	resource := &DashboardRangeMapOptions{}
	builder := &DashboardRangeMapOptionsBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *DashboardRangeMapOptionsBuilder) Build() (DashboardRangeMapOptions, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("DashboardRangeMapOptions", err)...)
	}

	if len(errs) != 0 {
		return DashboardRangeMapOptions{}, errs
	}

	return *builder.internal, nil
}

// Min value of the range. It can be null which means -Infinity
func (builder *DashboardRangeMapOptionsBuilder) From(from float64) *DashboardRangeMapOptionsBuilder {
	builder.internal.From = &from

	return builder
}

// Max value of the range. It can be null which means +Infinity
func (builder *DashboardRangeMapOptionsBuilder) To(to float64) *DashboardRangeMapOptionsBuilder {
	builder.internal.To = &to

	return builder
}

// Config to apply when the value is within the range
func (builder *DashboardRangeMapOptionsBuilder) Result(result cog.Builder[ValueMappingResult]) *DashboardRangeMapOptionsBuilder {
	resultResource, err := result.Build()
	if err != nil {
		builder.errors["result"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Result = resultResource

	return builder
}

func (builder *DashboardRangeMapOptionsBuilder) applyDefaults() {
}
