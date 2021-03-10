---
title: labelallow
---
# `labelallow` stage

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
relabel_configs:
  - action: replace
    source_labels:
      - __meta_kubernetes_pod_node_name
    target_label: node_name
  - action: replace
    source_labels:
      - __meta_kubernetes_namespace
    target_label: namespace
pipeline_stages:
- docker: {}    
- labelallow:
    - kubernetes_pod_name
    - 
```

Given the following log line:

```
log message\n
```

The first stage would append the value of the`kubernetes_pod_name` label into the beginning of the log line. 
The labeldrop stage would drop the label from being sent to Loki, and it would now be part of the log line instead.
