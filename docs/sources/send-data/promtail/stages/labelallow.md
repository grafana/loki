---
title: labelallow
menuTitle:  
description: The 'labelallow' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/labelallow/
weight:  
---

# labelallow

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The labelallow stage is an action stage that allows only the provided labels 
to be included in the label set that is sent to Loki with the log entry.

## Schema

```yaml
labelallow:
  - [<string>]
  ...
```

### Examples

For the given pipeline:

```yaml
kubernetes_sd_configs:
 - role: pod 
pipeline_stages:
- docker: {}    
- labelallow:
    - kubernetes_pod_name
    - kubernetes_container_name
```

Given the following incoming labels:

- `kubernetes_pod_name`: `"loki-pqrs"`
- `kubernetes_container_name`: `"loki"`
- `kubernetes_pod_template_hash`: `"79f5db67b"`
- `kubernetes_controller_revision_hash`: `"774858987d"`

Only the below labels would be sent to `loki`

- `kubernetes_pod_name`: `"loki-pqrs"`
- `kubernetes_container_name`: `"loki"`
