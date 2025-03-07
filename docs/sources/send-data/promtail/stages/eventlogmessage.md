---
title: eventlogmessage
menuTitle:  
description: The 'eventlogmessage' Promtail pipeline stage. 
aliases: 
- ../../../clients/promtail/stages/eventlogmessage/
weight:  
---

# eventlogmessage

{{< docs/shared source="loki" lookup="promtail-deprecation.md" version="<LOKI_VERSION>" >}}

The `eventlogmessage` stage is a parsing stage that extracts data from the Message string that appears in the Windows Event Log.

## Schema

```yaml
eventlogmessage:
  # Name from extracted data to parse, defaulting to the name
  # used by the windows_events scraper
  [source: <string> | default = message]

  # If previously extracted data exists for a key that occurs
  # in the Message, when true, the previous value will be
  # overwriten by the value in the Message. Otherwise,
  # '_extracted' will be appended to the key that is used for
  # the value in the Message.
  [overwrite_existing: <bool> | default = false]

  # When true, keys extracted from the Message that are not
  # valid labels will be dropped, otherwise they will be
  # automatically converted into valid labels replacing invalid
  # characters with underscores
  [drop_invalid_labels: <bool> | default = false]
```

The extracted data can hold non-string values and this stage does not do any
type conversions; downstream stages will need to perform correct type
conversion of these values as necessary. Please refer to the
[the `template` stage](../template/) for how to do this.

## Example combined with json

For the given pipeline:

```yaml
- json:
    expressions:
      message:
      Overwritten:
- eventlogmessage:
    source: message
    overwrite_existing: true
```

Given the following log line:

```
{"event_id": 1, "Overwritten": "old", "message": "Message type:\r\nOverwritten: new\r\nImage: C:\\Users\\User\\promtail.exe"}
```

The first stage would create the following key-value pairs in the set of
extracted data:

- `message`: `Message type:\r\nOverwritten: new\r\nImage: C:\Users\User\promtail.exe`
- `Overwritten`: `old`

The second stage will parse the value of `message` from the extracted data
and append/overwrite the following key-value pairs to the set of extracted data:

- `Image`: `C:\\Users\\User\\promtail.exe`
- `Message_type`: (empty string)
- `Overwritten`: `new`

