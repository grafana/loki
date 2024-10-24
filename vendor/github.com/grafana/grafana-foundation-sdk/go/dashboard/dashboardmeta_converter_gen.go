// Code generated - EDITING IS FUTILE. DO NOT EDIT.

package dashboard

import (
	"fmt"
	"strings"

	cog "github.com/grafana/grafana-foundation-sdk/go/cog"
)

func DashboardMetaConverter(input DashboardMeta) string {
	calls := []string{
		`dashboard.NewDashboardMetaBuilder()`,
	}
	var buffer strings.Builder
	if input.AnnotationsPermissions != nil {

		buffer.WriteString(`AnnotationsPermissions(`)
		arg0 := AnnotationPermissionConverter(*input.AnnotationsPermissions)
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.CanAdmin != nil {

		buffer.WriteString(`CanAdmin(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.CanAdmin))
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
	if input.CanSave != nil {

		buffer.WriteString(`CanSave(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.CanSave))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.CanStar != nil {

		buffer.WriteString(`CanStar(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.CanStar))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Created != nil {

		buffer.WriteString(`Created(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Created))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.CreatedBy != nil && *input.CreatedBy != "" {

		buffer.WriteString(`CreatedBy(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.CreatedBy))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Expires != nil {

		buffer.WriteString(`Expires(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Expires))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FolderId != nil {

		buffer.WriteString(`FolderId(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FolderId))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FolderTitle != nil && *input.FolderTitle != "" {

		buffer.WriteString(`FolderTitle(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FolderTitle))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FolderUid != nil && *input.FolderUid != "" {

		buffer.WriteString(`FolderUid(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FolderUid))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.FolderUrl != nil && *input.FolderUrl != "" {

		buffer.WriteString(`FolderUrl(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.FolderUrl))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.HasAcl != nil {

		buffer.WriteString(`HasAcl(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.HasAcl))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.IsFolder != nil {

		buffer.WriteString(`IsFolder(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.IsFolder))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.IsSnapshot != nil {

		buffer.WriteString(`IsSnapshot(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.IsSnapshot))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.IsStarred != nil {

		buffer.WriteString(`IsStarred(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.IsStarred))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Provisioned != nil {

		buffer.WriteString(`Provisioned(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Provisioned))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.ProvisionedExternalId != nil && *input.ProvisionedExternalId != "" {

		buffer.WriteString(`ProvisionedExternalId(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.ProvisionedExternalId))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.PublicDashboardEnabled != nil {

		buffer.WriteString(`PublicDashboardEnabled(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.PublicDashboardEnabled))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Slug != nil && *input.Slug != "" {

		buffer.WriteString(`Slug(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Slug))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Type != nil && *input.Type != "" {

		buffer.WriteString(`Type(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Type))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Updated != nil {

		buffer.WriteString(`Updated(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Updated))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.UpdatedBy != nil && *input.UpdatedBy != "" {

		buffer.WriteString(`UpdatedBy(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.UpdatedBy))
		buffer.WriteString(arg0)

		buffer.WriteString(")")

		calls = append(calls, buffer.String())
		buffer.Reset()

	}
	if input.Url != nil && *input.Url != "" {

		buffer.WriteString(`Url(`)
		arg0 := fmt.Sprintf("%#v", cog.Unptr(input.Url))
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

	return strings.Join(calls, ".\t\n")
}
