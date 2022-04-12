---
title: Storage Schema
---
# Loki Storage Schema

To support iterations over the storage layer, Loki has a configurable storage schema. The schema is defined in configuration with a `from` value that is used to mark the starting point of that schema, it is considered the active schema until another entry is inserted defining a new schema and a new `from` date.

![schema_example](../schema.png)

When querying data, Loki will use the defined schemas to automatically determine which format to use to query the data.

The schema design allows Loki to iterate over the storage layer without requiring migrations of existing data.

## Changing the schema

There are some very important considerations to make when changing the schema to prevent creating scenarios where data can’t be read.

### Always set the `from` date in the new schema to a date in the future.

The `from` date is interpreted by Loki to start at 00:00:00 UTC, therefore it’s very important when adding a new schema to always add a date that’s in the future so that Loki can transition to the new schema when that date and time arrives.

Be aware of using the current date and your relation to UTC to make sure UTC 00:00:00 has not already passed for the date you are using!

Example, the date is 2022-04-10 and you want to update to the v12 schema so you restart Loki with 2022-04-11 as the `from` date for the new schema, but you forgot to take into account that your timezone is UTC -5:00 and it’s currently 20:00 hours in your local timezone!  Which is actually 2022-04-11T01:00:00 UTC. When Loki starts it will see the new schema and begin to write and store objects following that new schema. If you try to query data that was written between 00:00:00 and 01:00:00 UTC, however, Loki will use the new schema and the data will be unreadable because it was actually created with the previous schema.

### You cannot “undo” or “rollback” a schema change.

Any data written with an active schema can only be read by that schema, instead you can add another new entry with the previous schema settings if required.

## Examples

```
schema_config:
    configs:
        - from: "2020-07-31"
          index:
            period: 24h
            prefix: loki_ops_index_
          object_store: gcs
          schema: v11
          store: boltdb-shipper
        - from: "2022-01-20"
          index:
            period: 24h
            prefix: loki_ops_index_
          object_store: gcs
          schema: v12
          store: boltdb-shipper
```
