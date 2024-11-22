// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[Snapshot] = (*SnapshotBuilder)(nil)

// A dashboard snapshot shares an interactive dashboard publicly.
// It is a read-only version of a dashboard, and is not editable.
// It is possible to create a snapshot of a snapshot.
// Grafana strips away all sensitive information from the dashboard.
// Sensitive information stripped: queries (metric, template,annotation) and panel links.
type SnapshotBuilder struct {
	internal *Snapshot
	errors   map[string]cog.BuildErrors
}

func NewSnapshotBuilder() *SnapshotBuilder {
	resource := &Snapshot{}
	builder := &SnapshotBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *SnapshotBuilder) Build() (Snapshot, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("Snapshot", err)...)
	}

	if len(errs) != 0 {
		return Snapshot{}, errs
	}

	return *builder.internal, nil
}

// Time when the snapshot expires, default is never to expire
func (builder *SnapshotBuilder) Expires(expires string) *SnapshotBuilder {
	builder.internal.Expires = expires

	return builder
}

// Is the snapshot saved in an external grafana instance
func (builder *SnapshotBuilder) External(external bool) *SnapshotBuilder {
	builder.internal.External = external

	return builder
}

// external url, if snapshot was shared in external grafana instance
func (builder *SnapshotBuilder) ExternalUrl(externalUrl string) *SnapshotBuilder {
	builder.internal.ExternalUrl = externalUrl

	return builder
}

// original url, url of the dashboard that was snapshotted
func (builder *SnapshotBuilder) OriginalUrl(originalUrl string) *SnapshotBuilder {
	builder.internal.OriginalUrl = originalUrl

	return builder
}

// Unique identifier of the snapshot
func (builder *SnapshotBuilder) Id(id uint32) *SnapshotBuilder {
	builder.internal.Id = id

	return builder
}

// Optional, defined the unique key of the snapshot, required if external is true
func (builder *SnapshotBuilder) Key(key string) *SnapshotBuilder {
	builder.internal.Key = key

	return builder
}

// Optional, name of the snapshot
func (builder *SnapshotBuilder) Name(name string) *SnapshotBuilder {
	builder.internal.Name = name

	return builder
}

// org id of the snapshot
func (builder *SnapshotBuilder) OrgId(orgId uint32) *SnapshotBuilder {
	builder.internal.OrgId = orgId

	return builder
}

// url of the snapshot, if snapshot was shared internally
func (builder *SnapshotBuilder) Url(url string) *SnapshotBuilder {
	builder.internal.Url = &url

	return builder
}

func (builder *SnapshotBuilder) Dashboard(dashboard cog.Builder[Dashboard]) *SnapshotBuilder {
	dashboardResource, err := dashboard.Build()
	if err != nil {
		builder.errors["dashboard"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.Dashboard = &dashboardResource

	return builder
}

func (builder *SnapshotBuilder) applyDefaults() {
}
