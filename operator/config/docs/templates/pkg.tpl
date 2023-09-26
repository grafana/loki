{{ define "packages" }}
---
title: "API"
description: "Generated API docs for the Loki Operator"
lead: ""
draft: false
images: []
menu:
+docs:
+parent: "operator"
weight: 1000
toc: true
---

This Document contains the types introduced by the Loki Operator to be consumed by users.

> This page is automatically generated with `gen-crd-api-reference-docs`.

{{ range .packages }}
    # {{ packageDisplayName . }} { #{{packageMDAnchorID . }} }
    {{ with (index .GoPackages 0 )}}
        {{ with .DocComments }}
        <div>
            {{ safe (renderComments .) }}
        </div>
        {{ end }}
    {{ end }}

    <b>Resource Types:</b>

    {{ range (visibleTypes (sortedTypes .Types))}}
        {{ template "type" .  }}
    {{ end }}
    <hr/>
    +newline
{{ end }}

{{ end }}

