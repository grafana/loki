---
title: Log Entry Deletion
weight: 60
---
# Log Entry Deletion

Log entry deletion is supported _only_ for the BoltDB Shipper index store.

Grafana Loki supports the deletion of log entries from a specified stream.
Log entries that fall within a specified time window and match an optional line filter are those that will be deleted.


The Compactor component exposes REST endpoints that process delete requests.
Hitting the endpoint specifies the streams and the time window.
The deletion of the log entries takes place after a configurable cancellation time period expires.

Log entry deletion relies on configuration of the custom logs retention workflow as defined in [Compactor](../retention#compactor). The Compactor looks at unprocessed requests which are past their cancellation period to decide whether a chunk is to be deleted or not.

## Configuration

Enable log entry deletion by setting `retention_enabled` to true and `deletion_mode` to `whole-stream-deletion`, `filter-only`, or `filter-and-delete` in the compactor's configuration. See the example in [Retention configuration](../retention#retention-configuration).

With `whole-stream-deletion`, all the log entries matching the query given in the delete request are removed.
With `filter-only`, log lines matching the query in the delete request are filtered out when querying Loki. They are not removed from the on-disk chunks.
With `filter-and-delete`, log lines matching the query in the delete request are filtered out when querying Loki, and they are also removed from the on-disk chunks.


A delete request may be canceled within a configurable cancellation period. Set the `delete_request_cancel_period` in the Compactor's YAML configuration or on the command line when invoking Loki. Its default value is 24h.

## Compactor endpoints

The Compactor exposes endpoints to allow for the deletion of log entries from specified streams.

### Request log entry deletion

```
POST /loki/api/v1/delete
PUT /loki/api/v1/delete
```

Query parameters:

* `query=<series_selector>`: query argument that identifies the streams from which to delete with optional line filters.
* `start=<rfc3339 | unix_timestamp>`: A timestamp that identifies the start of the time window within which entries will be deleted. If not specified, defaults to 0, the Unix Epoch time.
* `end=<rfc3339 | unix_timestamp>`: A timestamp that identifies the end of the time window within which entries will be deleted. If not specified, defaults to the current time.

A 204 response indicates success.

URL encode the `query` parameter. This sample form of a cURL command URL encodes `query={foo="bar"}`:

```
curl -g -X POST \
  'http://127.0.0.1:3100/loki/api/v1/delete?query={foo="bar"}&start=1591616227&end=1591619692' \
  -H 'x-scope-orgid: 1'

  The query parameter can also include filter operations. For example `query={foo="bar"} |= "other"` will filter out lines that contain the string "other" for the streams matching the stream selector `{foo="bar"}`.
```

### List delete requests

List the existing delete requests using the following API:

```
GET /loki/api/v1/delete
```

Sample form of a cURL command:

```
curl -X GET \
  <compactor_addr>/loki/api/v1/delete \
  -H 'x-scope-orgid: <orgid>'
```

This endpoint returns both processed and unprocessed requests. It does not list canceled requests, as those requests will have been removed from storage.

### Request cancellation of a delete request

Loki allows cancellation of delete requests until the requests are picked up for processing. It is controlled by the `delete_request_cancel_period` YAML configuration or the equivalent command line option when invoking Loki.

Cancel a delete request using this Compactor endpoint:

```
DELETE /loki/api/v1/delete
```

Query parameters:

* `request_id=<request_id>`: Identifies the delete request to cancel; IDs are found using the `delete` endpoint.

A 204 response indicates success.

Sample form of a cURL command:

```
curl -X DELETE \
  '<compactor_addr>/loki/api/v1/delete?request_id=<request_id>' \
  -H 'x-scope-orgid: <tenant-id>'
```
