---
title: Retention
---
# Loki Logs Deletion

_This feature is currently experimental and is only supported with the BoltDB Shipper index store._

Loki supports the deletion of log streams in a time range. Logs deletion requires running Compactor, which handles the delete request APIs(outlined below) and processing the delete requests.
Loki allows users to cancel the delete requests within the permitted cancellation period, which defaults to 24h and can be configured using `delete_request_cancel_period` YAML config or its equivalent CLI flag.

### How it works

Logs deletion relies on custom logs retention workflow define under [Compactor section in retention doc](../retention#compactor).
The only change to note is that Compactor would also look at un-processed requests which are past their cancellation period to decide whether it must delete the chunk or not.

### Configuration

Logs deletion can be enabled using the same config as retention i.e. `retention_enabled` as show in [sample retention config](../retention#retention-configuration).
The only relevant addition to config would be `delete_request_cancel_period`.

#### Requesting Deletion

Users can request the deletion of log streams using the following API:

```
POST /loki/api/admin/delete
PUT /loki/api/admin/delete
```

URL query parameters:

* match[]=<series_selector>: Repeated label matcher argument that selects the streams to delete. At least one match[] argument must be provided.
* start=<rfc3339 | unix_timestamp>: Start timestamp. Optional and defaults to minimum possible time.
* end=<rfc3339 | unix_timestamp>: End timestamp. Optional and defaults to now.

If the API call succeeds, a 204 is returned.

_Sample cURL command:_
```
curl -X POST \
  '<compactor_addr>/loki/api/admin/delete?match%5B%5D=%7Bfoo%3D%22bar%22%7D&start=1591616227&end=1591619692' \
  -H 'x-scope-orgid: <tenant-id>'
```

#### Listing Delete Requests

Users can list the created delete requests using the following API:

```
GET /loki/api/admin/delete
```

_Sample cURL command:_
```
curl -X GET \
  <compactor_addr>/loki/api/admin/delete \
  -H 'x-scope-orgid: <orgid>'
```

**NOTE:** List API returns processed and un-processed requests except the cancelled ones since they are removed from the store.

#### Cancellation of Delete Request

Loki allows cancellation of delete requests until they are not picked up for processing, controlled by the `delete_request_cancel_period` YAML config or its equivalent CLI flag.
Users can cancel a delete request using the following API:

```
POST /loki/api/admin/cancel_delete_request
PUT /loki/api/admin/cancel_delete_request
```

URL query parameters:

* request_id=<request_id>: Id of the request found using `delete_series` API.

If the API call succeeds, a 204 is returned.

_Sample cURL command:_
```
curl -X POST \
  '<compactor_addr>/loki/api/admin/cancel_delete_request?request_id=<request_id>' \
  -H 'x-scope-orgid: <tenant-id>'
```
