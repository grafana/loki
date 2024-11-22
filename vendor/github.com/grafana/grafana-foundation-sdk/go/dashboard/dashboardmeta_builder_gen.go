// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"time"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

var _ cog.Builder[DashboardMeta] = (*DashboardMetaBuilder)(nil)

type DashboardMetaBuilder struct {
	internal *DashboardMeta
	errors   map[string]cog.BuildErrors
}

func NewDashboardMetaBuilder() *DashboardMetaBuilder {
	resource := &DashboardMeta{}
	builder := &DashboardMetaBuilder{
		internal: resource,
		errors:   make(map[string]cog.BuildErrors),
	}

	builder.applyDefaults()

	return builder
}

func (builder *DashboardMetaBuilder) Build() (DashboardMeta, error) {
	var errs cog.BuildErrors

	for _, err := range builder.errors {
		errs = append(errs, cog.MakeBuildErrors("DashboardMeta", err)...)
	}

	if len(errs) != 0 {
		return DashboardMeta{}, errs
	}

	return *builder.internal, nil
}

func (builder *DashboardMetaBuilder) AnnotationsPermissions(annotationsPermissions cog.Builder[AnnotationPermission]) *DashboardMetaBuilder {
	annotationsPermissionsResource, err := annotationsPermissions.Build()
	if err != nil {
		builder.errors["annotationsPermissions"] = err.(cog.BuildErrors)
		return builder
	}
	builder.internal.AnnotationsPermissions = &annotationsPermissionsResource

	return builder
}

func (builder *DashboardMetaBuilder) CanAdmin(canAdmin bool) *DashboardMetaBuilder {
	builder.internal.CanAdmin = &canAdmin

	return builder
}

func (builder *DashboardMetaBuilder) CanDelete(canDelete bool) *DashboardMetaBuilder {
	builder.internal.CanDelete = &canDelete

	return builder
}

func (builder *DashboardMetaBuilder) CanEdit(canEdit bool) *DashboardMetaBuilder {
	builder.internal.CanEdit = &canEdit

	return builder
}

func (builder *DashboardMetaBuilder) CanSave(canSave bool) *DashboardMetaBuilder {
	builder.internal.CanSave = &canSave

	return builder
}

func (builder *DashboardMetaBuilder) CanStar(canStar bool) *DashboardMetaBuilder {
	builder.internal.CanStar = &canStar

	return builder
}

func (builder *DashboardMetaBuilder) Created(created time.Time) *DashboardMetaBuilder {
	builder.internal.Created = &created

	return builder
}

func (builder *DashboardMetaBuilder) CreatedBy(createdBy string) *DashboardMetaBuilder {
	builder.internal.CreatedBy = &createdBy

	return builder
}

func (builder *DashboardMetaBuilder) Expires(expires time.Time) *DashboardMetaBuilder {
	builder.internal.Expires = &expires

	return builder
}

func (builder *DashboardMetaBuilder) FolderId(folderId int64) *DashboardMetaBuilder {
	builder.internal.FolderId = &folderId

	return builder
}

func (builder *DashboardMetaBuilder) FolderTitle(folderTitle string) *DashboardMetaBuilder {
	builder.internal.FolderTitle = &folderTitle

	return builder
}

func (builder *DashboardMetaBuilder) FolderUid(folderUid string) *DashboardMetaBuilder {
	builder.internal.FolderUid = &folderUid

	return builder
}

func (builder *DashboardMetaBuilder) FolderUrl(folderUrl string) *DashboardMetaBuilder {
	builder.internal.FolderUrl = &folderUrl

	return builder
}

func (builder *DashboardMetaBuilder) HasAcl(hasAcl bool) *DashboardMetaBuilder {
	builder.internal.HasAcl = &hasAcl

	return builder
}

func (builder *DashboardMetaBuilder) IsFolder(isFolder bool) *DashboardMetaBuilder {
	builder.internal.IsFolder = &isFolder

	return builder
}

func (builder *DashboardMetaBuilder) IsSnapshot(isSnapshot bool) *DashboardMetaBuilder {
	builder.internal.IsSnapshot = &isSnapshot

	return builder
}

func (builder *DashboardMetaBuilder) IsStarred(isStarred bool) *DashboardMetaBuilder {
	builder.internal.IsStarred = &isStarred

	return builder
}

func (builder *DashboardMetaBuilder) Provisioned(provisioned bool) *DashboardMetaBuilder {
	builder.internal.Provisioned = &provisioned

	return builder
}

func (builder *DashboardMetaBuilder) ProvisionedExternalId(provisionedExternalId string) *DashboardMetaBuilder {
	builder.internal.ProvisionedExternalId = &provisionedExternalId

	return builder
}

func (builder *DashboardMetaBuilder) PublicDashboardEnabled(publicDashboardEnabled bool) *DashboardMetaBuilder {
	builder.internal.PublicDashboardEnabled = &publicDashboardEnabled

	return builder
}

func (builder *DashboardMetaBuilder) Slug(slug string) *DashboardMetaBuilder {
	builder.internal.Slug = &slug

	return builder
}

func (builder *DashboardMetaBuilder) Type(typeArg string) *DashboardMetaBuilder {
	builder.internal.Type = &typeArg

	return builder
}

func (builder *DashboardMetaBuilder) Updated(updated time.Time) *DashboardMetaBuilder {
	builder.internal.Updated = &updated

	return builder
}

func (builder *DashboardMetaBuilder) UpdatedBy(updatedBy string) *DashboardMetaBuilder {
	builder.internal.UpdatedBy = &updatedBy

	return builder
}

func (builder *DashboardMetaBuilder) Url(url string) *DashboardMetaBuilder {
	builder.internal.Url = &url

	return builder
}

func (builder *DashboardMetaBuilder) Version(version int64) *DashboardMetaBuilder {
	builder.internal.Version = &version

	return builder
}

func (builder *DashboardMetaBuilder) applyDefaults() {
}
