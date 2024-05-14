{{- define "packages" -}}

////
:_mod-docs-content-type: ASSEMBLY
include::_attributes/common-attributes.adoc[]
include::_attributes/attributes-openshift-dedicated.adoc[]
[id="logging-5-x-reference"]
= 5.x logging API reference
:context: logging-5-x-reference

toc::[]
////

////
** These release notes are generated from the content in the openshift/loki repository for the Loki Operator.
** Do not modify the content here manually except for the metadata and section IDs - changes to the content should be made in the source code.
////

{{ range .packages -}}

    {{- range (sortedTypes (visibleTypes .Types )) -}}

[id="logging-5-x-reference-{{ (typeDisplayName .) }}"]
== {{ (typeDisplayName .) }}

{{  (comments .CommentLines) }}
{{  template "type" (nodeParent . "") -}}

        {{end -}}

{{end -}}
{{end -}}
