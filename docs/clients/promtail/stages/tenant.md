# `tenant` stage

The tenant stage is an action stage that sets the tenant ID for the log entry
picking it from a field in the extracted data map. If the field is missing, the
default promtail client [`tenant_id`](../configuration.md#client_config) will
be used.


## Schema

```yaml
tenant:
  # Name from extracted data to whose value should be set as tenant ID
  source: <string>
```

### Example: extract the tenant ID from a structured log

For the given pipeline:

```yaml
pipeline_stages:
  - json:
      expressions:
        customer_id: customer_id
  - tenant:
      source: customer_id
```

Given the following log line:

```json
{"customer_id":"1","log":"log message\n","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}
```

The first stage would extract `customer_id` into the extracted map with a value of
`1`. The tenant stage would set the `X-Scope-OrgID` request header (used by Loki to
identify the tenant) to the value of the `customer_id` extracted data, which is `1`.


### Example: override the tenant ID based on the value of another field

For the given pipeline:

```yaml
pipeline_stages:
  - json:
      expressions:
        app:
        message:
  - labels:
      app:
  - match:
      selector: '{app="api"}'
      stages:
        - template:
            source: tenant_id
            template: 'team-api'
        - tenant:
            source: tenant_id
  - output:
      source: message
```

Given the following log line:

```json
{"app":"api","log":"log message\n","stream":"stderr","time":"2019-04-30T02:12:41.8443515Z"}
```

The pipeline would decode the JSON log, set the label `app="api"`, process the match
stage checking if the `{app="api"}` selector matches and - given it matches -
run the sub stages. The match's stages would set the field `tenant_id` with
value `"team-api"` into the extracted map, and finally the tenant stage would
override the tenant with the value of `tenant_id`, which is `"team-api"`.
