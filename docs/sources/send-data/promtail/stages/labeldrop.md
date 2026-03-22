---
title: labeldrop
menuTitle:  
description: The 'labeldrop' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/labeldrop/
weight:  
---

# labeldrop

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The labeldrop stage is an action stage that drops labels from
the label set that is sent to Loki with the log entry.

## Schema

```yaml
labeldrop:
  - [<string>]
  ...
```

### Examples

For the given pipeline:

```yaml
- replace:
    expression: "(.*)"
    replace: "pod_name:{{ .kubernetes_pod_name }} {{ .Value }}"
- labeldrop:
    - kubernetes_pod_name
```

Given the following log line:

```
log message\n
```

The first stage would append the value of the`kubernetes_pod_name` label into the beginning of the log line. 
The labeldrop stage would drop the label from being sent to Loki, and it would now be part of the log line instead.
