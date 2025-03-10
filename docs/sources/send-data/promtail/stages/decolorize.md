---
title: decolorize
menuTitle:  
description: The 'decolorize' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/decolorize/
weight:  
---

# decolorize

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `decolorize` stage is a transform stage that lets you strip
ANSI color codes from the log line, thus making it easier to
parse logs further.

There are examples below to help explain. 

## Decolorize stage schema

```yaml
decolorize:
  # Currently this stage has no configurable options
```

## Examples

The following is an example showing the use of the `decolorize` stage.

Given the pipeline:

```yaml
- decolorize:
```

Would turn each line having a color code into a non-colored one, e.g.

```
[2022-11-04 22:17:57.811] \033[0;32http\033[0m: GET /_health (0 ms) 204
```

is turned into

```
[2022-11-04 22:17:57.811] http: GET /_health (0 ms) 204
```
