---
title: eventlogmessage
description: eventlogmessage stage
---
# eventlogmessage

The `eventlogmessage` stage is a parsing stage that extracts data from the Message string that appears in the Windows Event Log.

## Schema

```yaml
eventlogmessage:
  # Name from extracted data to parse, defaulting to the name
  # used by the windows_events scraper
  [source: <string> | default = message]

  # When true, if previously extracted data exists for a key
  # found in the Message, it will be overwriten by the value
  # in the Message. Otherwise any such data will be ignored.
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
[the `template` stage]({{<relref "template">}}) for how to do this.


## Example

### Combined with json

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

