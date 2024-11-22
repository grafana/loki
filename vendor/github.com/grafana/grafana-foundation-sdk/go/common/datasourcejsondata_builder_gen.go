// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package common

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[DataSourceJsonData] = (*DataSourceJsonDataBuilder)(nil)

// TODO docs
type DataSourceJsonDataBuilder struct {
	internal *DataSourceJsonData
	errors   map[string]cog.BuildErrors
}

func NewDataSourceJsonDataBuilder() *DataSourceJsonDataBuilder {
	resource := &DataSourceJsonData{}
	builder := &DataSourceJsonDataBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *DataSourceJsonDataBuilder) Build() (DataSourceJsonData, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("DataSourceJsonData", err)...)
	}

	if len(errs) != 0 {
		return DataSourceJsonData{}, errs
	}

	return *builder.internal, nil
}

func (builder *DataSourceJsonDataBuilder) AuthType(authType string) *DataSourceJsonDataBuilder {
	builder.internal.AuthType = &authType

	return builder
}

func (builder *DataSourceJsonDataBuilder) DefaultRegion(defaultRegion string) *DataSourceJsonDataBuilder {
	builder.internal.DefaultRegion = &defaultRegion

	return builder
}

func (builder *DataSourceJsonDataBuilder) Profile(profile string) *DataSourceJsonDataBuilder {
	builder.internal.Profile = &profile

	return builder
}

func (builder *DataSourceJsonDataBuilder) ManageAlerts(manageAlerts bool) *DataSourceJsonDataBuilder {
	builder.internal.ManageAlerts = &manageAlerts

	return builder
}

func (builder *DataSourceJsonDataBuilder) AlertmanagerUid(alertmanagerUid string) *DataSourceJsonDataBuilder {
	builder.internal.AlertmanagerUid = &alertmanagerUid

	return builder
}

func (builder *DataSourceJsonDataBuilder) applyDefaults() {
}
