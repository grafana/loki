{{ define "member" }}
{{ if not (hiddenMember .)}}
{{ if fieldEmbedded . }}
{{ range .Type.Members }}
{{ template "member" . }}
{{ end }}
{{ else }}
<tr>
    <td>
        <code>{{ fieldName . }}</code><br/>
        <em>
            {{ if linkMDForType .Type }}
                <a href="{{ linkMDForType .Type }}">
                    {{ typeDisplayName .Type }}
                </a>
            {{ else }}
                {{ typeDisplayName .Type }}
            {{ end }}
        </em>
    </td>
    <td>
        {{ if isOptionalMember .}}
            <em>(Optional)</em>
        {{ end }}

        {{ safe (renderComments .CommentLines) }}

    {{ if and (eq (.Type.Name.Name) "ObjectMeta") }}
        Refer to the Kubernetes API documentation for the fields of the
        <code>metadata</code> field.
    {{ end }}

    </td>
</tr>
{{ end }}
{{ end }}
{{ end }}

{{ define "members" }}

{{ range .Members }}
{{ template "member" . }}
{{ end }}

{{ end }}
