---
title: output
menuTitle:  
description: The 'output' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/output/
weight:  
---

# output

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `output` stage is an action stage that takes data from the extracted map and
changes the log line that will be sent to Loki.

## Schema

```yaml
output:
  # Name from extracted data to use for the log entry.
  source: <string>
```

## Example

For the given pipeline:

```yaml
- json:
    expressions:
      user: user
      message: message
- labels:
    user:
- output:
    source: message
```

And the given log line:

```
{"user": "alexis", "message": "hello, world!"}
```

Then the first stage will extract the following key-value pairs into the
extracted map:

- `user`: `alexis`
- `message`: `hello, world!`

The second stage will then add `user=alexis` to the label set for the outgoing
log line, and the final `output` stage will change the log line from the
original JSON to `hello, world!`
