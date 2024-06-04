{{- define "type" -}}
    {{- if not (or .Type.IsPrimitive (eq (yamlType .Type) "string")) -}}
        {{- if .Members -}}
          {{- template "properties" .Type -}}
          {{- template "members" (nodeParent .Type .Path) -}}
        {{- end -}}
    {{- end -}}
{{- end -}}